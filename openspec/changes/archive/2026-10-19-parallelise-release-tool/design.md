## Context

See proposal.md for why. There are no spec changes (`skip_specs`): the `release-ci` requirements describe outcomes, which stay the same. This document covers how the three multi-module subcommands go parallel.

Today, in `tools/release`:

- `check.Run` loops over `plan.Changed` modules and returns at the first error (`checking <dir>: …`). A rule broken is a `report.Problem`, not an error; an error means a check couldn't run (git failed, gorelease couldn't be built or timed out). The gorelease runner is built lazily through a `*gorelease.Runner` field, "at most once".
- `publish.Run` loops over every module: parse `VERSION`, `Releases.Exists`, build the notes from git (`newRelease`), `Releases.Create`, then `Proxy.Announce`. A failure goes into that module's `Outcome.Err`, and the errors are joined at the end.
- `vulncheck.Scan` loops over modules and tools, keeping the results of the scans that completed and wrapping every failure in `ErrIncomplete`.

Facts about `github.com/alitto/pond/v2` (v2.7.3) that shape the approach, from its API docs:

- `NewResultPool[R](maxConcurrency, opts...)`, then `pool.NewGroup()`. `group.Wait()` returns the results in submission order, or the first error.
- On a task's error, queued tasks in the group are aborted, and `Wait` returns without waiting for the ones in flight.
- A task's panic is recovered by default and becomes an error (`pond.ErrPanic`), so it aborts the group like any other error.
- `maxConcurrency` 0 means no limit.
- `WithContext(ctx)` stops the pool when ctx ends; queued tasks then resolve with a cancellation error.
- `StopAndWait()` waits for every submitted task to finish.

## Goals / Non-Goals

**Goals:**

- Per-module work in `check`, `publish` and `vulncheck` runs concurrently, bounded by `--jobs`.
- Outputs are byte-for-byte what they'd be sequentially: same results, same order, same errors (joined, in module order).
- One module's failure, or panic, never stops another's work.

**Non-Goals:**

- Parallel GitHub writes. `publish` creates releases one at a time.
- `plan`: it diffs and selects, with no per-module action.
- Parallelism inside a module's work (gorelease or govulncheck themselves).

## Decisions

### 1. A `parallel` package owns pond

> Revised after code review: `Map` returns a value and an error per item; see [Map returns an outcome per item](#map-returns-an-outcome-per-item).

```go
// Map runs fn over items on a pool of at most jobs workers and returns the
// results in items' order. Each item's failure belongs in its result; one
// item failing doesn't stop the others. The error is non-nil only when ctx
// ended before every item ran, or when an fn panicked.
func Map[T, R any](ctx context.Context, jobs int, items []T, fn func(context.Context, T) R) ([]R, error)
```

Inside: a task per item that writes its result into its slot, on a `Group` (below) whose `Wait` calls `StopAndWait()`, so no task outlives the call, not even after a cancellation. Each task recovers its own panic and stores it as an error wrapping an exported `ErrPanic` that names the item's index and carries the stack. pond therefore never sees a failure, so it never aborts the queue. `Map` joins the panic errors and returns them alongside the results. A panic is a bug in the tool, so callers treat that error as fatal, not as one module's failure. `jobs` below 1 panics with a `parallel: Map:` message: a caller's bug, since `--jobs` is validated where it's parsed, and pond would read 0 as "no limit".

`publish` hands announcements to the pool one at a time as its serial creates finish, so it can't give `Map` the whole list up front. The package therefore also exports `Group` (`NewGroup(ctx, jobs)`, `Go(fn)`, `Wait()`), and `Map` is built on it. A `Group` is a plain pond pool with `WithContext`; each submitted function recovers its own panic, and `Wait` is `StopAndWait` followed by the joined errors: the context's error when some function never started, and one `ErrPanic` error per panic, naming its submission index. A pond group adds nothing once no task ever returns an error, so neither type uses one.

Keeping pond behind one package means the three commands share one set of semantics (order kept, nothing cancelled on failure, no task outliving the call), and those semantics are tested once.

**Alternatives:** pond groups in each command (three copies of the panic and cancellation handling); `golang.org/x/sync/errgroup` (`SetLimit` bounds it and, without `WithContext`, it cancels nothing, but `Wait` keeps only the first error, so per-item results and panic recovery would still be hand-written; the user chose pond); a hand-rolled semaphore and `sync.WaitGroup` (what pond already is, minus the tests).

### 2. `check` collects every module's error

`check.Run` maps `c.module` over the changed modules, giving each a `(Module, []report.Problem, error)`, then appends results and problems in module order and joins the errors, each still prefixed `checking <dir>:`. With any error it returns the joined error and no report, as today. The difference is that a pull request now learns of every module that couldn't be checked, not just the first.

The gorelease runner becomes a `sync.OnceValues` around `opts.Gorelease`, so concurrent modules build it once and all see the same result, error included. It's built with the context of the first module that needs it, which is the command's context in every caller.

### 3. `publish` reads in parallel and writes serially

> Revised in code review: GitHub lookups are serial too, and announcements run after every create rather than overlapping them; see [Fixes from code review](#fixes-from-code-review) and [Map returns an outcome per item](#map-returns-an-outcome-per-item).

```
modules ──▶ [pool] parse VERSION, Exists, newRelease (git) ──▶ pending, in module order
                                                                     │
          Create ── Create ── Create        serial, module order     ▼
            │         │         │
            ▼         ▼         ▼
       [pool] Announce, overlapping the next Create
```

1. **Prepare** (pool): everything up to and including `newRelease`. A module ends this phase failed, already released, or pending with its `Release` built.
2. **Create** (serial, in module order): `Releases.Create` for each pending module. GitHub's REST guidance is to make content-creating requests serially per token: concurrent release and tag creation risks secondary rate limits, and `publish` runs on every push to main, where a spurious 403 would leave a version unreleased until the next push.
3. **Announce** (pool): each successful create submits its `Proxy.Announce` to a pool as soon as it returns, so announcing overlaps the next create. `Run` waits for them all before returning.

Outcomes keep module order, and the joined error is built the same way as today, so `Markdown` and the warnings in `main.go` don't change.

### 4. `vulncheck` maps its scans

`Scan` maps `scanDir` over the directories into per-directory `(advisories, error)` pairs. It then keeps the completed results in directory order and wraps the joined failures in `ErrIncomplete`, exactly as today. This supersedes the `add-govulncheck` design's "declined: running the scans in parallel".

### 5. `--jobs` and `Options.Jobs`

`check`, `publish` and `vulncheck` each take `--jobs N`, default `runtime.GOMAXPROCS(0)`, and reject N below 1 with an error naming the flag before doing any work. Each command's `Options` gains `Jobs`; zero there means 1, so a test or caller filling `Options` by hand gets sequential behaviour, not a panic. The workflows keep the default, the runner's core count: most of the waiting is on the network or a child process.

### 6. Testing

- `parallel`: results keep order when later items finish first; at most `jobs` run at once (a counter with a high-water mark); a panic in one item doesn't stop the others, and the returned error wraps `ErrPanic` and names the item; a cancelled context returns its error after every running task has finished; `jobs` below 1 panics.
- Each command: an existing sequential test still passes with `Jobs` above 1; a slow fake shows modules overlap; a failure in one module still leaves the others' results. `publish`'s fake `Releases` records the order of `Create` calls and fails if two run at once.
- Everything under `go test -race`, and hand-written fakes made safe for concurrent use.

### Rollup into docs/release.md at archive

What runs in parallel and what doesn't (`publish`'s creates, and why), why one helper owns the pool's semantics, and why `check` now reports every module that couldn't be checked.

## Risks / Trade-offs

- [Interleaved child-process output] → None reaches the terminal: gorelease's and govulncheck's output is captured per module already.
- [Several gorelease or govulncheck runs at once contend for the runner's CPU and memory] → `--jobs` defaults to the core count; a module's work is mostly network and process waits. If CI slows, the workflows can pass `--jobs 1`.
- [A task that ignores its context keeps running after Ctrl-C] → Every task's long work is an `exec.CommandContext` or a ctx-aware API call, and `StopAndWait` keeps `Map` from returning before it ends.
- [Test flakiness from timing-based overlap tests] → Overlap is proven with channels (each task blocks until all have started), never with sleeps.

## Fixes from code review

During implementation, `parallel` gained a `Group` (submit as you go, then `Wait`) beside `Map`, so `publish` can start each announcement right after its create; `Map` is built on it. The first review found ten issues; nine were fixed and one declined.

- **Work that never ran is no longer mistaken for a result.** `Map` leaves an item that didn't run, after a cancellation or a panic, as R's zero value, and a zero `vulncheck` scan read as "scanned, nothing found". Each command's per-item result now carries a `done` flag. `vulncheck.Scan` reports undone directories as failures wrapping the new `ErrNotScanned`, keeps the completed scans (it had been discarding everything on a `Map` error), and joins `Map`'s error into `ErrIncomplete`.
- **`publish` makes every GitHub request serially (revises decision 3).** GitHub's guidance covers all of one token's requests, not only writes, so `Exists` moved out of the pool: `lookUp` runs serially in module order; only the release notes (git) and the proxy announcements use the pool. A module whose notes weren't built, cancelled or panicked, fails with the new `ErrNotPrepared` while the others still publish; a created version whose announcement never ran gets the new `ErrNotAnnounced` as its `ProxyErr`, a warning, so an interrupted announcement no longer makes the run fail. A panic in either pool still fails the run.
- **A panic names what it ran on.** `Group.Go` takes a label (`publish` passes the module's directory), and `Map` labels items `item <index>`, so a panic in an announcement no longer reports "task 0".
- **`parallel.NewGroup` is `parallel.New`**, per the constructor-naming rule: `Group` is the package's one type.
- **One `parallel.Jobs(n)`** replaces three copies of "zero means one" in `check`, `publish` and `vulncheck`, and clamps negatives too.
- **`--jobs` validation chains onto any existing `PreRunE`** instead of replacing it.
- **`check`'s gorelease temp directory is made up front**, so the build, which now runs on a worker inside `sync.OnceValues`, writes nothing the command function reads.
- **Declined: a smaller default for `--jobs`.** gorelease and govulncheck each use every core themselves, so `GOMAXPROCS` of them at once can oversubscribe a runner; the user kept `GOMAXPROCS`, since most of each run waits on the network, and the workflows can pass `--jobs` if CI slows.

## Map returns an outcome per item

After the review, the user found `publish.Run` hard to read: `done` flags in per-command result structs, an `announced` slice, a `notDone` helper and two sentinels, all to recover what `Map` threw away, namely which items never ran or panicked. `Map` now returns that itself:

```go
type Result[R any] struct {
	Value R
	Err   error
}

func Map[T, R any](ctx context.Context, jobs int, items []T, fn func(context.Context, T) (R, error)) []Result[R]
```

An item's `Err` is fn's error, or wraps the new `ErrNotRun` (with the context's cause) when it never ran, or `ErrPanic` (with the value and stack) when it panicked. There's no second return: every caller already treats any item's error as a failure of that module, so a joined error would only repeat them. The type is `parallel.Result`, not `MapResult`, by the same no-stutter rule as the constructors.

That made `Group` unnecessary. It existed only so `publish` could start each announcement while the next release was created; `publish` now announces in one `Map` after the last create, giving up an overlap worth a second or two for the one to three versions a push publishes. With it went `New`, the labels, `publish`'s `prepared.done`, `notDone`, `ErrNotPrepared` and `ErrNotAnnounced`, `vulncheck`'s `scan` struct and `ErrNotScanned` (`parallel.ErrNotRun` carries it now), and `check`'s outcome struct. `parallel` is back to the single function decision 1 designed, with per-item errors.

One behaviour changed: a panic inside a proxy announcement is now that version's `ProxyErr`, a warning like any other announcement failure, instead of failing the run. Announcing is best-effort already. A panic anywhere else still fails its module, and so the run.

## Second review

- **A panic is a `*parallel.PanicError`** wrapping `ErrPanic`. Its message holds only the value, since it reaches `publish`'s summary cells and annotations; its `Stack` field keeps the stack, which `main` prints to standard error for the run's error and for any announcement's.
- **`ErrNoResult`**: `Map` sets an item's error before calling fn and overwrites it only when fn returns, so a fn that calls `runtime.Goexit` no longer reads as a success with a zero value (in `vulncheck`, a clean scan).
- **An announcement that never ran or panicked names its module and version** (`announcing <path>@<version>: …`). One that ran keeps `Announce`'s own error, which names them already.
- **The announce-after-create test can fail now**: the fake proxy rejects any announcement arriving before the last release exists, instead of waiting for it.
- **Docs**: the `--jobs` default is the runner's core count, not "two"; the errgroup rationale is that `Wait` keeps only the first error, not that it's fail-fast.
- **Follow-ups left open**: `jobs` is guarded three times (`jobsFlag`, `parallel.Jobs`, `Map`'s panic, which nothing can reach), resolved in the final review; and `check` still drops every module's report when one couldn't be checked, as before this change, where returning the partial report beside the error, as `vulncheck` does, would show a pull request its broken rules sooner.

## Final review

Eight findings: six fixed, two declined.

- **An interrupted `publish` fails.** With announcements batched after the creates, a signal in that phase turned every announcement into a not-run warning and the run exited 0. `Run` now adds `publishing interrupted: <cause>` to its error whenever its context has ended.
- **One guard for `jobs`.** `Map` clamps below 1 to 1 itself (pond reads 0 as no limit), so `parallel.Jobs` and `Map`'s panic are gone, and `--jobs` is a `pflag.Value` that rejects anything below 1, or not a number, as the flag is parsed. That also removes the `PreRunE` wrapping, which would have silently disabled a plain `PreRun` and depended on call order.
- **No side channel in the announcements.** The `bool` that told "Announce's error" from "Map's error" is replaced by checking the error for `parallel`'s sentinels.
- **Every panic's stack is printed**, not just the first in a joined error.
- **The "never overlaps" tests detect overlap reliably.** A guard holds its slot for 20 ms, so a concurrent call finds it taken instead of slipping between two quick calls; a test drives two concurrent calls through it to prove it catches them.
- **Declined: a smaller `--jobs` default**, as twice before.
- **Declined: a lazy gorelease temp directory.** Making it eager removed a write from a worker goroutine; laziness would bring that back for the saving of one `mkdir` per run.

## What actually happened

`check`, `publish` and `vulncheck` run their per-module work on a pond pool, bounded by `--jobs` (default `GOMAXPROCS`), with outputs identical to a sequential run: `vulncheck --dry-run` matched across `--jobs 1` and `--jobs 4` on the real repo, and so did `check` against an older base. Deviations from the decisions above, each recorded in its section below:

- `parallel` went through three shapes: `Map` returning a slice and an error (decision 1); then `Map` plus a `Group`, so `publish` could announce each version right after its create; and finally a single `Map` returning a `Result{Value, Err}` per item, with `ErrNotRun`, `ErrPanic` (a `*PanicError` carrying the stack) and `ErrNoResult`. Per-item errors made every command's "did this item run?" bookkeeping disappear.
- `publish` makes every GitHub request serially, lookups as well as creates (decision 3 had lookups on the pool), and announces in a batch after the last create rather than overlapping creates. A panicking announcement is a warning; an interrupted run fails.
- `Map` clamps `jobs` itself, and `--jobs` is validated as it's parsed, instead of `Options.Jobs` helpers and a `PreRunE`.
- `check` checks every module and joins the errors of those it couldn't check, as decided.

## Follow-ups left open

- `check` still drops every module's report when one module couldn't be checked; returning the partial report beside the error, as `vulncheck` does, would show a pull request its broken rules sooner.
- Concurrent gorelease runs were exercised only by unit tests: the real-repo comparison's modules failed a version rule first, so gorelease never ran. Watch the first tier-release pull request's `check` timing.
- The `--jobs` default stays `GOMAXPROCS` (kept three times in review). If CI slows or gorelease starts timing out, pass `--jobs` in the workflows.
- `add-govulncheck`'s design records "declined: parallel scans"; when that change is archived, its record should point here.

## Migration Plan

One pull request off main. No workflow changes are needed, since the default `--jobs` applies. Merging publishes nothing.
