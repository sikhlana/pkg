## Why

The repo has release CI but no library module, so `plan`, `check` and `publish` have only ever run against fixture repositories. `cli` is the smallest useful module in idio/go-modules, which makes it a good first release: small enough to review closely, and real enough to take the release flow end to end by hand. It's rebuilt rather than copied. Since the original was written, Go has started stamping version-control data into binaries and naming the signal in `signal.NotifyContext`'s cause. The original also has gaps: a nil-error panic, a second Ctrl-C that does nothing, and two version outputs that disagree.

## What Changes

- New module `cli` (`github.com/sikhlana/pkg/cli`), starting at 0.1.0. It provides Cobra scaffolding for a program's `main`:
  - **A signal-aware context.** The first SIGINT or SIGTERM cancels it, and `context.Cause` names the signal. The handler is then released, so a second signal ends the process.
  - **Execute.** Runs a root command with a context and turns its error into a process exit code: 0 on success, the code carried by an `ExitError` in the error chain, and 1 otherwise. A carried code of zero or less becomes 1, so a failed command never exits 0.
  - **ExitError and one constructor, `Exit(code, err)`.** It works with `errors.New` and `fmt.Errorf` instead of mirroring them. A nil `err` is allowed, and its message is `exit status N`. Cobra still prints errors; a command that wants to exit quietly sets cobra's `SilenceErrors`.
  - **A root command factory.** It sets the name and short description, and a persistent `--config`/`-c` flag whose name is an exported constant. It also turns usage text off for runtime errors and sets a `--version` flag that prints the build line.
  - **A version subcommand.** It prints the same line as `--version` and takes the program name from the root command.
  - **Build info.** Values set with `-ldflags -X` take precedence, then whatever the go command stamped into the binary (main module version, VCS revision, commit time, modified flag), then `dev`.
- How it differs from go-modules' `cli`:
  - `SignalContext` takes a parent context, and a second signal kills the process.
  - `Exit(code, err)` replaces `Exit(code, text)` and `Exitf`.
  - An `ExitError` with a nil `Err` no longer panics.
  - Build info falls back to `debug.ReadBuildInfo`. `GitCommit` and `BuildDate` become `Revision` and `Time`, and `Time` is the commit time, not the build time.
  - `NewVersionCommand` takes no name.
- Constitution: the receiver rule gets an exception for error types, which take pointer receivers even when small and immutable.
- Root README gets its first Modules row and loses the "Nothing has shipped yet" line. `go.work` registers `./cli`.
- The tasks end with a by-hand check of the release flow: failures to trigger on the pull request, the published release and the proxy after merge, a publish re-run that should change nothing, and a follow-up pull request to test the bump rule.

## Capabilities

### New Capabilities

- `cli`: signal handling, exit-code mapping, the root and version commands, and how build info is resolved.

### Modified Capabilities

None. `release-ci` runs on its first real module, but none of its requirements change.

## Impact

- **`cli` (new module):** all of its exported API is new. Its exposure shape is plain functions plus an error type and a build-info value. It has no fx Module, no Config and no runner; it never touches fx.
- **Other library modules:** none exist yet.
- **`tools/release`:** unchanged, now exercised end to end.
- **New dependencies (`cli` only):**
  - spf13/cobra, which brings in spf13/pflag and inconshreveable/mousetrap;
  - testify, for tests only.
- **Docs and repo files:**
  - `openspec/config.yaml` (the receiver exception);
  - `README.md` (Modules row);
  - `go.work` (`use ./cli`).
- **Release:** merging publishes `cli/v0.1.0` as an immutable GitHub release and asks the module proxy for it.
