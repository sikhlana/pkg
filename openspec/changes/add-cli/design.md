## Context

The source is `cli` in idio/go-modules: 117 lines over cobra, at 0.2.0 there, with no importers inside that repo. See proposal.md for why it's the first module here and why it's rebuilt rather than copied. The requirements are in specs/cli/spec.md. This document covers how they're met and what was rejected.

Constraints that shape the approach:

- `testpackage` forces external test packages, and `reassign` (pattern `.*`) flags any test that assigns to a package variable such as `cli.Version`. The original needed six nolints to test its version output.
- `-ldflags -X` can only set package-level string variables that are uninitialised or initialised to a constant.
- Cobra decides whether to print an error and usage after `RunE` returns. It reads `SilenceErrors` and `SilenceUsage` from both the root and the subcommand that ran (confirmed in cobra's `ExecuteC`). It adds `--version` only to a command whose `Version` is set, just before parsing flags.
- `signal.NotifyContext` cancels its context before releasing the handler, and since Go 1.26 it gives the context a cause that names the signal, in an unexported type. `signal.Stop` restores the action a signal had when the process started: the default, or ignored for a process started with it ignored (Go's os/signal docs). Since Go 1.24, `go build` stamps the main module's version from the VCS tag, or a pseudo-version with `+dirty`. Since Go 1.18 it also stamps `vcs.revision`, `vcs.time` and `vcs.modified` when the build is inside the repository.

## Goals / Non-Goals

**Goals:**

- An API shaped by Go 1.27 and this repo's constitution, not by the original.
- Behaviour that can be fully tested without mutating globals or nolints.

**Non-Goals:**

- **fx integration.** `cli` runs before any fx app exists and has nothing to provide or run.
- **Mapping a signal to exit code 128+n.** A service that shuts down cleanly should exit 0. A command that wants 130 can read the signal from the `*SignalError` cause and return `Exit(128+n, nil)`, silencing cobra's error output.
- **Loading configuration.** `--config` carries a path; reading the file belongs to a future configloader port.
- **Windows signal tests.** The code compiles and works there, since `os.Interrupt` and `syscall.SIGTERM` exist on every platform, but the tests that send signals are Unix-only.

## Decisions

### 1. Our own handler, released before cancelling

`SignalContext(parent)` subscribes a one-slot channel to `os.Interrupt` and `syscall.SIGTERM` and starts one goroutine, which waits for whichever comes first:
- **A signal.** It calls `signal.Stop` first, then cancels the context with a `*SignalError` cause.
- **The context ends** (the parent is done, or `stop` was called). It calls `signal.Stop`.

The returned `stop` calls `signal.Stop` itself before cancelling, so the handler is gone when `stop` returns. Calling it twice is harmless.

Because the handler is released before cancellation, anything that observes the context done, a shutdown hook say, already sees the original action restored. A second Ctrl-C then kills the process with the conventional status. The only window left is between the goroutine receiving the first signal and calling `signal.Stop`, nanoseconds before anything can observe the cancellation. The test needs no delay between "cancelled" and the second signal.

`SignalError` carries the signal and matches `context.Canceled` with `errors.Is`, as the standard library's own cause does. Exporting it means a program can read the signal, to log it or map it to an exit code, without parsing a message.

Taking a parent means tests pass `t.Context()`, and a program can tie signal handling to a narrower context.

Alternatives:
- **Leave the handler in place until `main` returns,** as the original did. A hung shutdown can then only be ended with `kill -9`.
- **`signal.NotifyContext` plus `context.AfterFunc(ctx, stop)`,** the first implementation. It reuses the standard library, but NotifyContext cancels first and releases later, on another goroutine. A second signal arriving in between is swallowed, and the test needed a fixed sleep that could fail at random on a loaded runner. Code review rejected it. Owning the roughly 20 lines that put `Stop` before `cancel` costs less than living with the gap.
- **A `signal.Notify` loop that calls `os.Exit` on the second signal.** It picks an exit code on the program's behalf and skips deferred calls on purpose. Restoring the original action gets the same result with the operating system's own status.

### 2. Execute maps errors and prints nothing

`Execute(ctx, cmd)` gives `ctx` to every command in `cmd.Root()`'s tree, calls the root's `ExecuteContext(ctx)` and returns:
- 0 for nil;
- the code of the first `*ExitError` that `errors.AsType` finds, or 1 if it's outside 1 to 255;
- 1 for any other error.

**Why the context goes to every command.** Cobra hands its context only to commands whose context is still unset. A tree executed twice, typical in a consumer's tests, would otherwise give its subcommands the first run's context, possibly already cancelled. Executing a subcommand would also run the root with `context.Background()`. Walking the tree costs a few lines and makes the spec's "every command observes the context" hold unconditionally. Cobra's lazily added help and completion commands pick the context up from the root on their first run, and from the walk after that.

**Why codes are clamped to 1–255.** The first implementation passed codes above 255 through. Code review found that Unix keeps only the low 8 bits, so `Exit(256, err)` exited 0, breaking the promise that a failure never exits 0. Clamping loses Windows' wider range, which portable command-line tools don't use. `ExitError` normalises through one unexported method, which both `Error()` and `Execute` use, so the `exit status N` message always names the code the process really exits with.

Cobra keeps printing errors, as decided in explore. `ErrPrefix`, `SetErr` and per-command overrides all keep working. A quiet exit (grep- or diff-style) doesn't need a mechanism in `cli`: the command sets `cmd.SilenceErrors = true` and returns `Exit(1, nil)`, and cobra reads that field after `RunE` returns. Execute printing errors itself was considered and rejected. It would have duplicated cobra's output plumbing to support one case cobra already handles.

### 3. One constructor: Exit(code, err)

`Exit(code int, err error) error` replaces the original's `Exit(code, text)` and `Exitf(code, format, args...)`. Callers build the cause with `errors.New` or `fmt.Errorf` as usual, so there's no second format-string API, and no vet warning when the message is a variable. A nil `err` is valid: `Error()` returns `exit status N`, the same wording as `exec.ExitError`, instead of dereferencing nil.

It returns `error`, always holding an `*ExitError`, not the concrete pointer. The first implementation returned `*ExitError` so callers could read `Code` directly. Code review found that this invites helpers typed `func() *cli.ExitError`, whose nil result becomes a non-nil `error`: cobra then panics calling `Error()`, and so does `Execute` reading `Code`. Returning `error` follows the Go FAQ's advice on nil error values. Callers that need the code use `errors.AsType`.

Alternatives: keeping all three constructors (familiar, but three ways to build one value), or only the struct literal (smallest API, wordy at every call site).

### 4. ExitError uses pointer receivers; the constitution gets an exception

The constitution's receiver rule calls for value receivers on small immutable structs, and `ExitError` is one. But with value receivers, both `ExitError` and `*ExitError` would satisfy `error`. An `errors.AsType[*ExitError]` would then miss a value in the chain, and the reverse is also true. The standard library uses pointers for its error types (`fs.PathError`, `exec.ExitError`, `url.Error`) for exactly this reason. The constitution's Code standards gain one clause: error types use pointer receivers so `errors.AsType` has a single target. No linter enforces receiver kind by role, so it stays a written rule.

### 5. Usage text off at the root

`NewRootCommand` sets `SilenceUsage: true`. Cobra honours the root's setting for every subcommand, so runtime failures stop printing the flag help.

The alternative was a trick that turns `SilenceUsage` on inside `PersistentPreRun`, so usage still prints for flag errors. It has two holes. Cobra validates required flags and flag groups after the pre-run hooks, so those errors would lose their usage anyway. And a subcommand's own `PersistentPreRun` replaces the root's unless `EnableTraverseRunHooks` is set. The trade-off accepted: a bad flag prints `Error: unknown flag: --x` without the usage block. The message names the flag, and `--help` still works.

### 6. --config stays, without a shorthand; its name is a constant

The persistent `--config` flag carries over, and its name is exported as `ConfigFlag`, so commands read it with `cmd.Flags().GetString(cli.ConfigFlag)` instead of repeating the string. No accessor function: it would only hide an error that means "this root wasn't built by `NewRootCommand`".

**It has no shorthand.** The original's `-c` was dropped in code review. A persistent shorthand defined by a library claims that letter on every command under the root, and pflag panics when a subcommand defines it again (say `-c` for `--concurrency`). An app can't take it back either: pflag records shorthands when a flag is added, so changing `Shorthand` afterwards doesn't register anything. There's also no standard to follow:
- nginx, gunicorn, golangci-lint, ESLint and Envoy use `-c`;
- cobra-cli's generated root, the Docker CLI (where `-c` is `--context`), Prometheus and Telegraf have no shorthand;
- `-c` also means "command string" (`sh -c`, `python -c`) and "set one key" (`git -c`, `postgres -c`).

Without a shorthand, every letter stays with the program.

### 7. Build info: string variables for -X, a struct for everything else

- **Link-time variables:** `Version`, `Revision` and `Time`, package-level strings defaulting to `""`. Empty means "not set at link time". `dev` is applied when resolving, not as the variable's initial value, so an unset variable can be told apart from one set to `dev`. The names follow the go command's `vcs.revision` and `vcs.time` keys.
- **`BuildInfo`:** a small value type with `Version`, `Revision`, `Time` and `Modified`. Its value-receiver `String()` renders the version line without the program name. `ReadBuildInfo() BuildInfo` resolves it, and it never fails, so unlike `debug.ReadBuildInfo` it returns no `ok`.
- **Fields resolve independently.** A CI job that sets only `Version` through `-X` still gets the revision and time from VCS stamping. A main module version of `(devel)` counts as unset.
- **`Modified` comes only from `vcs.modified`.** `-X` can't set a bool, and a string variable meaning "true" would be a weaker second source.
- **`Time` stays a string.** Link-time values arrive in whatever format the build passes, and parsing would reject or drop them. A consumer that needs a `time.Time` parses `vcs.time`'s RFC 3339 itself.
- **The revision is shown in full.** Shortening it is a presentation choice the build can make by passing a short `-X` value.
- **No caching.** `ReadBuildInfo` runs at most twice per process (root construction and the version subcommand), and `debug.ReadBuildInfo` is cheap.

Other modules that want a version tag, such as a future logger or metrics module, get a `BuildInfo` from the app, following the constitution's rule that the app wires integrations. They don't import `cli`.

Alternative: keep the original's three ldflags variables, defaulting to `dev` and `unknown`. A plain `go build` then reports nothing it could have known, `BuildDate` (the time of the build) makes builds non-reproducible, and tests have to mutate globals.

### 8. One version line, rendered once

`NewRootCommand` sets cobra's `Version` to `ReadBuildInfo().String()` and the version template to `{{.DisplayName}} {{.Version}}` plus a newline. `NewVersionCommand()` prints `cmd.Root().DisplayName()`, a space and the root's `Version`, falling back to `ReadBuildInfo().String()` when that's empty. Both lines read the root's `Version`, so they agree even after the program changes it. The subcommand still works under a root that wasn't built by `NewRootCommand`.

The first implementation had the subcommand resolve `ReadBuildInfo()` on its own. Code review pointed out that it then drifted from `--version` as soon as a program set the root's `Version` after construction.

The template only references cobra's fields, so build values are data, never template text: a `{{` in a link-time value can't break the template. `DisplayName` rather than `Name` matches cobra's own help and version output when a program sets the display-name annotation (plugin-style `kubectl foo`).

Alternative: drop the subcommand and keep only `--version`. It's kept because `myapp version` is a common convention and costs a few lines.

### 9. Tests without global mutation

- **The resolver is a pure function.** It takes the link-time values and the result of `debug.ReadBuildInfo`, and is reached from `cli_test` through `export_test.go`, which `testpackage` allows by default. That covers precedence, `(devel)`, missing VCS keys and `Modified` without touching `Version`, `Revision` or `Time`.
- **Signal tests run only on Unix** (`//go:build unix`), and none of them call `t.Parallel`.
  - The first-signal test sends SIGINT, then SIGTERM, to its own process. It checks `ctx.Done()` and that `context.Cause` is a `*SignalError` carrying that signal and matching `context.Canceled`.
  - The second-signal test re-runs the test binary as a child, selected by an environment variable. The child sets up `SignalContext`, prints a ready line, waits for cancellation and prints "cancelled". The parent sends SIGINT, waits for "cancelled", sends SIGTERM straight away, and checks that the child's wait status shows it was killed by SIGTERM.
  - The second signal is SIGTERM because a background job started from a script inherits SIGINT ignored, and a released handler restores that. Code review reproduced the failure with `bash -c './cli.test … & wait'`.
  - No sleep: decision 1 guarantees the handler is released before "cancelled" can print.
  - A `t.Cleanup` waits on the child, so a failed check never leaves it unreaped, and its output is read to EOF before `Wait`.
- **Cobra tests** build commands with `NewRootCommand` and capture output with `SetOut`, `SetErr` and `SetArgs`. They cover: error printed without usage, quiet exit, `--version` matching the subcommand, and `--config` reaching a subcommand.
- **Checked by hand, not in tests:** whether `-X` and VCS stamping actually reach the binary is go-command behaviour, so it's in the task list. Automating it would need a scratch module with a `replace` directive and an offline module cache, which is heavy for confirming what the toolchain already guarantees.

### 10. Main and CommandFunc

Added in the follow-up release (0.2.0). It's also what tests the bump rule: new exports make gorelease suggest a minor version.

Every program's `main.go` repeated the same lines: start the signal context, defer `stop`, run `Execute`, all inside a helper so that `os.Exit` runs after the deferred `stop`. `Main(root CommandFunc)` is those lines, written once. `CommandFunc` is `func() *cobra.Command`, the shape a program's `cmd.Root` already has, so `main` is `cli.Main(cmd.Root)`.

- **A function, not a command.** `Main` calls `root` after the signal handler is installed. A signal that arrives while the tree is being built then cancels the context instead of killing the process.
- **The work happens in an unexported `run`.** `os.Exit` skips deferred calls, so `run` defers `stop` and returns the code, and `Main` only exits. That's also the split gocritic's `exitAfterDefer` asks for.
- **A named type.** `func Main(root func() *cobra.Command)` behaves the same with one export fewer. `CommandFunc` was kept because it names what a program passes, and gives the godoc somewhere to say it's called once.
- **No context or options parameter.** `main` has no parent context, and nothing yet needs configuring. A program that needs something else between the steps uses `SignalContext` and `Execute` directly, as the README shows.

Alternatives:
- **`Main(root *cobra.Command)`,** which builds the tree before signal handling exists.
- **A plain function parameter,** which drops the named type (above).

Tests:
- `run` is reached through `export_test.go`, for exit codes and the context commands get.
- A Unix-only test sends SIGINT from inside the `CommandFunc`. It would kill the test binary if the handler weren't already installed; instead the command sees a `*SignalError` cause.
- `Main` itself runs in a child re-run of the test binary, which must exit 7. A child that exits 0 can't be used, because Go's testing package fails a test that calls `os.Exit(0)`.

### 11. Dependencies

- **spf13/cobra:** subcommands, persistent flags, help, usage and shell completion. Scaffolding over it is the module's purpose, so hand-rolling isn't an option. It brings in spf13/pflag and inconshreveable/mousetrap (Windows only).
- **stretchr/testify:** tests only, as the constitution requires.

## Risks / Trade-offs

- **[Another signal subscriber keeps the handler]** If the program also calls `signal.Notify` for SIGINT, releasing ours doesn't restore the original action, and a second Ctrl-C is swallowed. → Godoc and README say so.
- **[Started with SIGINT ignored]** A background job started from a script ignores SIGINT again once the handler is released, so a second Ctrl-C does nothing there. → That's the disposition its parent chose. SIGTERM still ends it, and the godoc and README say so.
- **[Reimplementing NotifyContext]** We own about 20 lines the standard library already has. → They're covered by the signal tests, and owning them buys the release-before-cancel ordering (decision 1).
- **[Windows exit codes above 255]** Clamping to 1–255 discards codes Windows could carry. → Portable command-line tools stay within 1–255.
- **[Usage on flag errors]** Silencing usage at the root hides it for flag errors too. → The error names the flag, and `--help` works.
- **[Docker builds without .git]** With `.git` excluded from the build context, nothing gets stamped and the line reads `myapp dev`. → The README shows the `-X` flags for such builds.
- **[`+dirty` twice]** A modified checkout at a tag renders as `v1.2.3+dirty (…, modified)`. → Accepted: rewriting the go command's stamped version would hide what it recorded.
- **[Immutable v0.1.0]** Once published, v0.1.0 can't be replaced. → Before 1.0 a breaking fix is a minor (0.2.0), and a broken version can be retracted from go.mod.

## Migration Plan

There are no consumers of `github.com/sikhlana/pkg/cli`. Programs moving off `github.com/idio/go-modules/cli` change the import path and:

| go-modules | here |
|---|---|
| SignalContext() | SignalContext(context.Background()) |
| Exit(code, text) | Exit(code, errors.New(text)) |
| Exitf(code, format, args...) | Exit(code, fmt.Errorf(format, args...)) |
| a code above 255 | exits 1 |
| -c path | --config path |
| GitCommit, BuildDate | Revision, Time (commit time, from VCS when not set) |
| NewVersionCommand(name) | NewVersionCommand() |
| func main() { os.Exit(run()) } with SignalContext, Execute | func main() { cli.Main(cmd.Root) } |

Merging publishes `cli/v0.1.0`. Rollback means fixing forward: 0.1.1, or 0.2.0 for an API fix, plus a `retract` directive if v0.1.0 is unusable.

## Rollup at archive

- **Constitution:** the error-type receiver exception (decision 4) is written in this change, not at archive.
- **docs/:** none expected; the rest of this rationale is specific to `cli` and lives in its spec and README. If a later module consumes `BuildInfo`, the "app passes build info along" pattern may earn a `docs/` page then.
