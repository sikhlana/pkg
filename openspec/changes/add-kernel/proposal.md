## Why

`cli`, `config/configloader` and `log` are published, and each left its integration to a kernel: the log flags and event logger, tint as the default text handler, `--config` into the loader. Nothing runs an app's loops yet either. go-modules' `kernel` does both jobs in one module, which every loop module imports to register its runner, so here that one import would pull cobra, viper and log into every server and receiver. It's rebuilt as two modules rather than copied: a small runner every loop module can afford, and the wiring only apps import. The rebuild also fixes what the original gets wrong: a runner that exits cleanly leaves the rest running, a panicking runner skips every stop hook, `fx.Shutdowner` requests vanish, and a JSON service ends its log stream with a plain-text error line.

## What Changes

- New module `kernelrun` (`github.com/sikhlana/pkg/kernelrun`), starting at 0.1.0. It runs an fx app's runners between its start and stop:
  - **Runners.** Modules and apps register named runners into an fx group, as a constructor or a value. A runner is a `func(ctx) error`.
  - **The first runner to return stops the rest.** Its siblings' context is cancelled and `Run` waits for them all. A runner that ignores cancellation is left to the caller's second signal.
  - **Clean or crash.** A runner returning nil is clean, and so is one returning its context's own error once that context is done: cancelled, or past the caller's deadline. Anything else, including a recovered panic or an exit through `runtime.Goexit`, is a crash, named after its runner, and the app still shuts down cleanly.
  - **Checked before start.** No runners, a duplicate name, an empty name or a nil function fails with an exported sentinel before any hook starts.
  - **Start.** A start timeout bounds the OnStart hooks. A panicking constructor or invoke becomes an error. A build or startup failure is reported as its root cause, without dig's scaffolding. A start cut short by the caller's context ending is a clean exit; any other start failure is reported.
  - **Stop.** A stop timeout bounds the OnStop hooks, on a context detached from the caller's cancellation that keeps its values. The run's outcome rides on it.
  - **`kernelrun/runerr`.** OnStop hooks read the run's outcome with `runerr.From`, and tests attach one with `runerr.With`. Hooks rolled back after a failed start read `runerr.ErrNotStarted`, never a clean nil. It imports only the standard library.
  - **Signals.** `Run` never subscribes to OS signals; the caller's context is the only way one reaches it, so `cli`'s second Ctrl-C still ends the process.
  - **Logging.** Run and stop failures are logged at Error, and the lifecycle at Debug, through the graph's `*slog.Logger` when there is one.
- New module `kernel` (`github.com/sikhlana/pkg/kernel`), starting at 0.1.0 once `kernelrun` is published. It turns cobra commands into apps:
  - **A root command** from `cli`'s, with `--debug`, `--quiet`/`-q` (mutually exclusive) and `--format`, whose default the app picks.
  - **Commands** that build the app from the flags: `log.Module` with the level, format and text handler (tint unless the app picks another), fx's events through `log.NewEventLogger`, and `--config` into `configloader.WithFilename`. Shared options apply to every command, and each command adds its own.
  - **The default logger.** slog's default writes through the app's logger while it runs, so third-party `slog` and stdlib `log` output share its format, and the previous default comes back afterwards.
  - **Output.** Failures after the app is built aren't printed again by cobra: the event logger reports startup failures and `kernelrun` reports run and stop failures. Every line stays in the chosen format, and the exit code still comes from the error.
- How it differs from go-modules' `kernel`:
  - Split in two; loop modules depend only on `kernelrun`.
  - `RunFunc`, `kernel/multi` and `ProvideRunFunc` merge into one runner group that `Run` always runs. A single main loop is one runner.
  - The first return stops everything, where errgroup only stopped on an error.
  - `WithVerbose` goes: the log level decides what fx's events show, and a startup error is always its root cause, since the event logger logs the full chain.
  - New: recovered panics, the start timeout, clean exits on cancellation during start, the checks before start, the context-preserving stop, and the wiring with `cli`, `log` and `configloader`.
  - A runner's `Canceled` or `DeadlineExceeded` counts as clean only once its context is actually done, not whenever a runner returns it.
  - The `runerr` package keeps `With` and `From`, under `kernelrun`.
- Constitution:
  - **The run-loop rule names `kernelrun`.** A loop module's `ProvideRunner` registers through `kernelrun.ProvideRunner`. The module prepares in OnStart (binding a listener, say), serves in the runner and cleans up in OnStop, so a setup failure is a startup failure and a finished `Start` means ready.
  - **`fx.Shutdowner` is banned,** through forbidigo: `kernelrun` doesn't listen for it, and returning from a runner ends the app.
  - **`kernelrun` and `kernel` have no `Module` variable.** They build and run the app rather than join its graph, like `config/configloader`'s exception.
- Root README gains two Modules rows, and `go.work` registers both modules.
- Delivered in two pull requests: `kernelrun` with the constitution and lint edits, then `kernel` once `kernelrun/v0.1.0` is published.

## Capabilities

### New Capabilities

- `kernelrun`: runner registration and its checks, concurrent running, the first return stopping the rest, clean and crash outcomes, recovered panics, start and stop timeouts, the stop context and the run outcome on it, signal independence, and logging.
- `kernel`: the root command's flags, building each command's app from them, shared and per-command options, the default logger and its restoration, and how failures are reported.

### Modified Capabilities

None. `cli`, `config/configloader` and `log` keep their contracts; the kernel uses them as published.

## Impact

- **`kernelrun` (new module):** all of its exported API is new. Its exposure shapes are plain functions (`Run`, and `runerr.From` and `runerr.With`), a group with its constant and registration helpers, a function type for a runner, and exported sentinels. It has no `Module` (the constitution exception above), no Config and no runner of its own.
- **`kernel` (new module):** all of its exported API is new. Its exposure shape is a value built once from developer settings, with methods that build cobra commands and run them, plus exported flag-name constants. It has no `Module`, no Config and no runner.
- **`cli`, `config/configloader`, `config/configvalidator`, `log`, `tools/release`:** unchanged.
- **New dependencies:**
  - `kernelrun`: go.uber.org/fx, and go.uber.org/dig for the root cause.
  - `kernel`: spf13/cobra, `cli`, `log` (with `log/tint`), `config/configloader` and `kernelrun`.
  - Both: testify, for tests only.
- **Docs and repo files:**
  - `openspec/config.yaml` (the three constitution edits);
  - `.golangci.yml` (the `fx.Shutdowner` ban);
  - `README.md` (two Modules rows);
  - `go.work` (`use ./kernel`, `use ./kernelrun`);
  - `docs/kernel.md`, rolled up at archive: why two modules, why the first return stops everything, how failures are reported, and the lifecycle rule for loop modules.
- **Release:** merging the first pull request publishes `kernelrun/v0.1.0`; merging the second publishes `kernel/v0.1.0`.
