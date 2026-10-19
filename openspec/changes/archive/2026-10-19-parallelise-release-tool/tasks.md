## 1. Setup

- [x] 1.1 Branch `parallelise-release-tool` off `origin/main` after `git fetch`, with #23 merged.
- [x] 1.2 In `tools/release`, `GOWORK=off go get github.com/alitto/pond/v2@v2.7.3`, then `go mod tidy`.

## 2. parallel

- [x] 2.1 `parallel/parallel.go`: `Map` and `ErrPanic` per design decision 1: one result pool per call with `WithContext`, one group, per-task panic recovery that never returns an error to pond, a deferred `StopAndWait`, results in item order, joined panic errors, the context's error when it ended first, and a panic for `jobs` below 1.
- [x] 2.2 `parallel/parallel_test.go`: order kept when later items finish first; at most `jobs` at once (high-water mark); a panic doesn't stop the others and names its item; a cancelled context returns after every running task finished; `jobs` below 1 panics. Overlap proven with channels, never sleeps.

## 3. check

- [x] 3.1 `Options.Jobs` (zero means 1); `Run` maps `c.module` over the changed modules, appends results and problems in module order, and returns the errors joined, each prefixed `checking <dir>:`.
- [x] 3.2 Replace the `*gorelease.Runner` field with a `sync.OnceValues` around `opts.Gorelease`.
- [x] 3.3 Tests: the existing ones with `Jobs` above 1; two modules that both fail to run report both errors; the gorelease runner is built once across concurrent modules.

## 4. publish

- [x] 4.1 `Options.Jobs`; split `publishModule` into prepare (parse, `Exists`, `newRelease`), create and announce per design decision 3: prepare on the pool, `Create` serially in module order, each successful create's `Announce` submitted to a pool and awaited before `Run` returns. Outcomes keep module order; errors joined as today.
- [x] 4.2 Tests: the existing ones with `Jobs` above 1; a fake `Releases` that fails if two `Create` calls overlap and records their order; announcements overlap later creates; one module's failure leaves the others' outcomes.

## 5. vulncheck

- [x] 5.1 `Options.Jobs`; `Scan` maps `scanDir` over the directories, keeping completed results in directory order and wrapping joined failures in `ErrIncomplete`.
- [x] 5.2 Tests: the existing ones with `Jobs` above 1; scans overlap; a failure still returns the others' results.

## 6. main

- [x] 6.1 A `--jobs` flag on `check`, `publish` and `vulncheck`, default `runtime.GOMAXPROCS(0)`, rejected below 1 before any work with an error naming the flag; passed to each command's `Options.Jobs`.

## 7. Docs

- [x] 7.1 `tools/release/README.md`: `--jobs` on the three commands, and that `publish` creates releases one at a time.
- [x] 7.2 `docs/release.md`: a section on what runs in parallel and why `publish`'s creates don't, from the rollup list in design.md.

## 8. Verification

- [x] 8.1 In `tools/release/` with `GOWORK=off`: `go generate ./...` if a mocked interface changed, `modernize -fix ./...`, `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), `go test ./...`, `go test -race ./...`, `go tool govulncheck ./... tool`.
- [x] 8.2 `release vulncheck --dry-run --jobs 4` and `release check --base origin/main --jobs 4` locally against the real repo, with the same reports as `--jobs 1`.
- [x] 8.3 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat both verification steps.

## 9. Code review fixes

See design.md's Fixes from code review.

- [x] 9.1 Mark per-item results with `done`; `vulncheck.Scan` keeps completed scans on a `Map` error and reports undone directories with `ErrNotScanned`.
- [x] 9.2 Move `publish`'s `Exists` calls to a serial `lookUp`; build release notes on the pool; `ErrNotPrepared` for undone notes, `ErrNotAnnounced` for announcements that never ran; only panics fail the run.
- [x] 9.3 Label `Group.Go`; rename `NewGroup` to `New`; add `parallel.Jobs`.
- [x] 9.4 Chain `jobsFlag`'s validation onto an existing `PreRunE`; make `check`'s gorelease temp directory up front.
- [x] 9.5 Tests: serial lookups, a cancelled announcement only warning, `vulncheck` keeping completed scans after a cancellation and a panic, a label in a panic, `Jobs`, the `PreRunE` chain.
- [x] 9.6 README, `docs/release.md` and the proposal updated.
- [x] 9.7 Re-run section 8.

## 10. Map returns an outcome per item

See design.md's Map returns an outcome per item.

- [x] 10.1 `parallel.Map` returns `[]Result[R]`, with `ErrNotRun` and `ErrPanic` per item; remove `Group`, `New` and the labels.
- [x] 10.2 `check`, `vulncheck` and `publish` use per-item errors; `publish` announces in a second `Map` after every create; remove the `done` flags, `notDone`, `ErrNotPrepared`, `ErrNotAnnounced` and `ErrNotScanned`.
- [x] 10.3 Tests rewritten for the per-item contract; README, `docs/release.md`, the proposal and design.md updated.
- [x] 10.4 Re-run section 8.

## 11. Final review fixes

See design.md's Final review.

- [x] 11.1 Fail an interrupted `publish` run.
- [x] 11.2 Clamp `jobs` inside `Map`; remove `parallel.Jobs` and its panic; make `--jobs` a `pflag.Value`.
- [x] 11.3 Decide announcement error prefixes by `parallel`'s sentinels instead of a `bool`.
- [x] 11.4 Print every panic's stack.
- [x] 11.5 Replace the overlap tests' `Gosched` with a guard, and test the guard.
- [x] 11.6 Re-run section 8.
