## 1. kernelrun scaffold

Sections 1 to 5 are the first pull request. `kernelrun` and `kernel` are new, so each starts at 0.1.0 with no bump task, and no other module changes. Neither has mocked interfaces, so there's no go generate task. Nothing is copied from go-modules' `kernel`: code, godoc and comments are all written here, from what this code does.

- [x] 1.1 Branch `add-kernelrun` off `origin/main` after `git fetch`.
- [x] 1.2 Create `kernelrun/`:
  - `go.mod` for `github.com/sikhlana/pkg/kernelrun`, with the go directive from `.go-version`;
  - `VERSION` containing `0.1.0`;
  - require the latest go.uber.org/fx, go.uber.org/dig and stretchr/testify.
  - Done when `go build ./...` passes from `kernelrun/`.
- [x] 1.3 Register `./kernelrun` in the root `go.work`.
- [x] 1.4 Settle the fx questions the design relies on, each with a test that stays:
  - one private `fx.Module` per `ProvideRunner` call, registered twice in one app without colliding;
  - `fx.RecoverFromPanics` at the top level turning a constructor's panic into the app's error.

## 2. Runners

- [x] 2.1 `Func`, `Runner`, `RunnerGroup`, `ProvideRunner` and `WithRunner`, with `ProvideRunner`'s adapter taking a params struct. See design decision 2.
- [x] 2.2 Tests: a runner from a constructor taking a graph value, a runner from a value, and two `ProvideRunner` calls in one app.

## 3. Run and runerr

- [x] 3.1 `kernelrun/runerr`: `With` and `From`, importing only `context`.
- [x] 3.2 `Options`, `DefaultStartTimeout`, `DefaultStopTimeout` and the unexported resolvers.
- [x] 3.3 `Run`, as in design decision 3:
  - `fx.New` with `RecoverFromPanics`, the caller's options and an invoke collecting the runners and the optional logger;
  - a build failure as `starting the app:` and its root cause;
  - the checks, with `ErrNoRunners`, `ErrDuplicateRunner` and `ErrInvalidRunner`;
  - `Start` under the start timeout, a failure after the caller cancelled returning nil;
  - the runners on a context cancelled, with a cause, when the first returns; waiting for all; clean and crash per runner; panics recovered with their stack;
  - `Stop` on `context.WithoutCancel` with the stop timeout and `runerr.With`, and the joined result.
- [x] 3.4 Logging, as in design decision 5: crashes, panics and stop failures at Error, the lifecycle at Debug, nothing without a logger, and no startup failures.
- [x] 3.5 Tests for the spec's requirements, with a recording handler as the logger:
  - start before runners, the caller's values, the first return stopping the rest in all three ways;
  - every outcome scenario, including an early cancellation error and an error type surviving through `errors.As`;
  - a panicking runner, with OnStop hooks run;
  - the three checks, each with an OnStart hook that never runs;
  - a constructor's error without dig's scaffolding, a constructor's panic, the start timeout, and a cancellation during start;
  - the stop context's values, liveness and outcome, a failing OnStop hook joined with a crash, and `runerr` on a bare context;
  - the log records for a crash and the lifecycle, and no panic without a logger.
- [x] 3.6 Signal independence: a re-run of the test binary runs an app whose runner waits on its context, sends itself SIGINT, and the parent checks that it ended by SIGINT.

## 4. Constitution, lint and docs

- [x] 4.1 In `openspec/config.yaml`:
  - the run-loop rule: a loop module's `ProvideRunner` registers through `kernelrun.ProvideRunner` under its `DefaultRunnerName`, and the module prepares in OnStart, serves in the runner and cleans up in OnStop;
  - the `Module` rule's exception covers `kernelrun` and `kernel`, which build and run the app rather than join it;
  - Tooling: `fx.Shutdowner` is banned, since nothing listens for it and returning from a runner ends the app.
- [x] 4.2 In `.golangci.yml`, a forbidigo rule for `fx.Shutdowner` with a message saying to return from a runner. Confirm it reports a parameter of that type in a throwaway file, then delete the file.
- [x] 4.3 Write `kernelrun/README.md` in the constitution's order:
  - overview;
  - Configuration: none, with the timeouts as developer settings;
  - Exported Members: every export of `kernelrun` and `kernelrun/runerr`, with the `Runner` and `Options` fields;
  - Usage, Standalone: `Run` with a runner from `WithRunner`;
  - Usage, With fx: a loop module's `ProvideRunner` and the lifecycle rule, a one-shot runner, reading `runerr.From` in an OnStop hook, and that `fx.Shutdowner` is never heard.
  - No backticks in table cells.
- [x] 4.4 In the root `README.md`, add the `kernelrun` row to the Modules table in alphabetical order.

## 5. kernelrun verification

- [x] 5.1 In `kernelrun/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 5.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 5.1 and 5.2.

## 6. kernel scaffold

Sections 6 to 9 are the second pull request, and wait for `kernelrun/v0.1.0` to be published.

- [ ] 6.1 Once `kernelrun/v0.1.0` is published, branch `add-kernel-wiring` off `origin/main` after `git fetch`.
- [ ] 6.2 Create `kernel/`:
  - `go.mod` for `github.com/sikhlana/pkg/kernel`, with the go directive from `.go-version`;
  - `VERSION` containing `0.1.0`;
  - require the published `kernelrun`, `cli`, `log` and `config/configloader`, and the latest spf13/cobra, go.uber.org/fx and stretchr/testify.
  - Done when `go build ./...` passes from `kernel/`, and with `GOWORK=off`.
- [ ] 6.3 Register `./kernel` in the root `go.work`.
- [ ] 6.4 Settle the cobra question with a test that stays: `--debug` and `--quiet`, made mutually exclusive on the root, are rejected together on a subcommand.

## 7. Commands and wiring

- [ ] 7.1 `DebugFlag`, `QuietFlag`, `FormatFlag`, `ErrMissingFlag`, `Options`, `App`, `New` and `RootCommand`: `cli`'s root, `--debug`, `--quiet` with `-q`, mutual exclusion, and `--format` as a `TextVar` defaulting to `Options.Format`. See design decisions 6 and 7.
- [ ] 7.2 `Run`, as in design decision 7: the flags read from `cmd.Flags()` (`--format` with `GetText`), a missing one failing with `ErrMissingFlag`; the options in the design's order; slog's default saved and restored around `kernelrun.Run`; and `cmd.SilenceErrors` set whenever `kernelrun.Run` returns an error.
- [ ] 7.3 `Command`, taking no arguments and running the app with its options.
- [ ] 7.4 Tests through `cli.Execute`, with a recording destination supplied in `Shared` and runners from `kernelrun.WithRunner`:
  - the root's flags: `-q` on a subcommand, `--debug --quiet` together, `--format yaml`, and the program's default format;
  - the app: the level, the program's text handler, tint by default, fx's events under `--debug`, `--config` reaching the loader, and shared and own options together;
  - a root without the kernel's flags, and a shortcut command given an argument;
  - slog's default during the run, and restored after a clean run and after a startup failure;
  - a crash printing nothing and exiting 1, `cli.Exit(3, …)` exiting 3, a startup failure printing nothing, and an unknown flag still printed.

## 8. kernel docs

- [ ] 8.1 Write `kernel/README.md` in the constitution's order:
  - overview;
  - Configuration: none, with `Options` as developer settings and the flags as per-run choices;
  - Exported Members: every export, with the `Options` fields and the `App` methods;
  - Usage: a `main` with `cli.Main`, `RootCommand`, a shortcut command, a command with an argument through `Run`, config through `Shared`, and the kernel owning `configloader.WithFilename`;
  - how failures are reported.
  - No backticks in table cells.
- [ ] 8.2 In the root `README.md`, add the `kernel` row to the Modules table in alphabetical order.

## 9. kernel verification

- [ ] 9.1 In `kernel/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [ ] 9.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 9.1 and 9.2.
