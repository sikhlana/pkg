## Context

The source is `cli` in idio/go-modules: 117 lines over cobra, at 0.2.0 there, with no importers inside that repo. See proposal.md for why it's the first module here and why it's rebuilt rather than copied. The requirements are in specs/cli/spec.md. This document covers how they're met and what was rejected.

Constraints that shape the approach:

- `testpackage` forces external test packages, and `reassign` (pattern `.*`) flags any test that assigns to a package variable such as `cli.Version`. The original needed six nolints to test its version output.
- `-ldflags -X` can only set package-level string variables that are uninitialised or initialised to a constant.
- Cobra decides whether to print an error and usage after `RunE` returns. It reads `SilenceErrors` and `SilenceUsage` from both the root and the subcommand that ran (confirmed in cobra's `ExecuteC`). It adds `--version` only to a command whose `Version` is set, just before parsing flags.
- Since Go 1.26, a context cancelled by `signal.NotifyContext` has a cause that names the signal. Since Go 1.24, `go build` stamps the main module's version from the VCS tag, or a pseudo-version with `+dirty`. Since Go 1.18 it also stamps `vcs.revision`, `vcs.time` and `vcs.modified` when the build is inside the repository.

## Goals / Non-Goals

**Goals:**

- An API shaped by Go 1.27 and this repo's constitution, not by the original.
- Behaviour that can be fully tested without mutating globals or nolints.

**Non-Goals:**

- **fx integration.** `cli` runs before any fx app exists and has nothing to provide or run.
- **Mapping a signal to exit code 128+n.** A service that shuts down cleanly should exit 0. A command that wants 130 can check `context.Cause` and return `Exit(130, …)`.
- **Loading configuration.** `--config` carries a path; reading the file belongs to a future configloader port.
- **Windows signal tests.** The code compiles and works there, since `os.Interrupt` and `syscall.SIGTERM` exist on every platform, but the tests that send signals are Unix-only.

## Decisions

### 1. NotifyContext, plus AfterFunc to release the handler

`SignalContext(parent)` calls `signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)` and registers `context.AfterFunc(ctx, stop)`. Whatever ends the context (a signal, the parent, or `stop`) also releases the handler. Once no other subscriber remains, the signal's default action comes back, so a second Ctrl-C kills the process with the conventional status. The returned `stop` is still the caller's to defer, for the normal path where no signal arrives. Calling it twice is harmless.

Taking a parent means tests pass `t.Context()`, and a program can tie signal handling to a narrower context.

Alternatives:
- **Leave the handler in place until `main` returns,** as the original did. A hung shutdown can then only be ended with `kill -9`.
- **Our own `signal.Notify` loop that calls `os.Exit` on the second signal.** That reimplements NotifyContext, picks an exit code on the program's behalf, and skips deferred calls on purpose. Restoring the default handler gets the same result with less code and the operating system's own status.

### 2. Execute maps errors and prints nothing

`Execute(ctx, cmd)` calls `cmd.ExecuteContext(ctx)` and returns:
- 0 for nil;
- the code of the first `*ExitError` that `errors.AsType` finds, raised to 1 if it's zero or negative;
- 1 for any other error.

Codes above 255 are passed through: Unix truncates them, Windows doesn't, and either way it's the caller's choice.

Cobra keeps printing errors, as decided in explore. `ErrPrefix`, `SetErr` and per-command overrides all keep working. A quiet exit (grep- or diff-style) doesn't need a mechanism in `cli`: the command sets `cmd.SilenceErrors = true` and returns `Exit(1, nil)`, and cobra reads that field after `RunE` returns. Execute printing errors itself was considered and rejected. It would have duplicated cobra's output plumbing to support one case cobra already handles.

### 3. One constructor: Exit(code, err)

`Exit(code int, err error) *ExitError` replaces the original's `Exit(code, text)` and `Exitf(code, format, args...)`. Callers build the cause with `errors.New` or `fmt.Errorf` as usual, so there's no second format-string API, and no vet warning when the message is a variable. A nil `err` is valid: `Error()` returns `exit status N`, the same wording as `exec.ExitError`, instead of dereferencing nil.

It returns the concrete `*ExitError` so tests and callers can read `Code` directly. It never returns nil, so the typed-nil interface trap can't happen.

Alternatives: keeping all three constructors (familiar, but three ways to build one value), or only the struct literal (smallest API, wordy at every call site).

### 4. ExitError uses pointer receivers; the constitution gets an exception

The constitution's receiver rule calls for value receivers on small immutable structs, and `ExitError` is one. But with value receivers, both `ExitError` and `*ExitError` would satisfy `error`. An `errors.AsType[*ExitError]` would then miss a value in the chain, and the reverse is also true. The standard library uses pointers for its error types (`fs.PathError`, `exec.ExitError`, `url.Error`) for exactly this reason. The constitution's Code standards gain one clause: error types use pointer receivers so `errors.AsType` has a single target. No linter enforces receiver kind by role, so it stays a written rule.

### 5. Usage text off at the root

`NewRootCommand` sets `SilenceUsage: true`. Cobra honours the root's setting for every subcommand, so runtime failures stop printing the flag help.

The alternative was a trick that turns `SilenceUsage` on inside `PersistentPreRun`, so usage still prints for flag errors. It has two holes. Cobra validates required flags and flag groups after the pre-run hooks, so those errors would lose their usage anyway. And a subcommand's own `PersistentPreRun` replaces the root's unless `EnableTraverseRunHooks` is set. The trade-off accepted: a bad flag prints `Error: unknown flag: --x` without the usage block. The message names the flag, and `--help` still works.

### 6. --config stays; its name is a constant

The persistent `--config`/`-c` flag carries over, and its name is exported as `ConfigFlag`, so commands read it with `cmd.Flags().GetString(cli.ConfigFlag)` instead of repeating the string. No accessor function: it would only hide an error that means "this root wasn't built by `NewRootCommand`". The shorthand isn't exported; nothing reads a flag by its shorthand.

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

`NewRootCommand` sets cobra's `Version` to `ReadBuildInfo().String()` and the version template to `{{.DisplayName}} {{.Version}}` plus a newline. `NewVersionCommand()` prints `cmd.Root().DisplayName()`, a space and `ReadBuildInfo().String()`. Both lines come from the same `String()`, so they agree. The subcommand doesn't depend on the root having been built by `NewRootCommand`.

The template only references cobra's fields, so build values are data, never template text: a `{{` in a link-time value can't break the template. `DisplayName` rather than `Name` matches cobra's own help and version output when a program sets the display-name annotation (plugin-style `kubectl foo`).

Alternative: drop the subcommand and keep only `--version`. It's kept because `myapp version` is a common convention and costs a few lines.

### 9. Tests without global mutation

- **The resolver is a pure function.** It takes the link-time values and the result of `debug.ReadBuildInfo`, and is reached from `cli_test` through `export_test.go`, which `testpackage` allows by default. That covers precedence, `(devel)`, missing VCS keys and `Modified` without touching `Version`, `Revision` or `Time`.
- **Signal tests run only on Unix** (`//go:build unix`), and none of them call `t.Parallel`.
  - The first-signal test sends SIGINT to its own process and checks `ctx.Done()` and `context.Cause`.
  - The second-signal test re-runs the test binary as a child, selected by an environment variable. The child sets up `SignalContext`, prints a ready line and waits. The parent sends SIGINT twice and checks that the child's wait status shows it was killed by SIGINT.
- **Cobra tests** build commands with `NewRootCommand` and capture output with `SetOut`, `SetErr` and `SetArgs`. They cover: error printed without usage, quiet exit, `--version` matching the subcommand, and `--config` reaching a subcommand.
- **Checked by hand, not in tests:** whether `-X` and VCS stamping actually reach the binary is go-command behaviour, so it's in the task list. Automating it would need a scratch module with a `replace` directive and an offline module cache, which is heavy for confirming what the toolchain already guarantees.

### 10. Dependencies

- **spf13/cobra:** subcommands, persistent flags, help, usage and shell completion. Scaffolding over it is the module's purpose, so hand-rolling isn't an option. It brings in spf13/pflag and inconshreveable/mousetrap (Windows only).
- **stretchr/testify:** tests only, as the constitution requires.

## Risks / Trade-offs

- **[Another signal subscriber keeps the handler]** If the program also calls `signal.Notify` for SIGINT, releasing ours doesn't restore the default action, and a second Ctrl-C is swallowed. → Godoc and README say so.
- **[AfterFunc window]** `stop` runs on its own goroutine after cancellation, so a second signal in the microseconds between them is swallowed. → Accepted; a person pressing Ctrl-C twice is far slower.
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
| GitCommit, BuildDate | Revision, Time (commit time, from VCS when not set) |
| NewVersionCommand(name) | NewVersionCommand() |

Merging publishes `cli/v0.1.0`. Rollback means fixing forward: 0.1.1, or 0.2.0 for an API fix, plus a `retract` directive if v0.1.0 is unusable.

## Rollup at archive

- **Constitution:** the error-type receiver exception (decision 4) is written in this change, not at archive.
- **docs/:** none expected; the rest of this rationale is specific to `cli` and lives in its spec and README. If a later module consumes `BuildInfo`, the "app passes build info along" pattern may earn a `docs/` page then.
