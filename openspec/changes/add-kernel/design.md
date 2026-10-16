## Context

The source is `kernel` in idio/go-modules (0.4.3 there): `kernel.New(opts).Run(ctx, opts...)` around a single `RunFunc`, `kernel/multi` running a group of named runners with errgroup, `kernel/runerr` carrying the run's outcome to OnStop hooks, and a `WithVerbose` switch for fx's events and error wrapping. See proposal.md for why it's rebuilt as two modules rather than copied; the requirements are in specs/kernelrun/spec.md and specs/kernel/spec.md. This document covers how they're met and what was rejected.

Facts that shape the approach, checked against sources:

- **Every loop module in go-modules imports `kernel/multi`** (`http/server`, `messaging`, `broadcast`, `pipeline`), and nothing outside `kernel` reads `runerr`.
- **`app.Start` never subscribes to OS signals;** fx has a test that guarantees it. `app.Done` and `app.Wait` do, and they're the only listeners for `fx.Shutdowner`, whose `Shutdown` broadcasts to them (`shutdown.go:86`). An app driven by `Start` and `Stop` never hears a shutdown request.
- **A failed `Start` rolls back with the context it was given** (`app.go:684`), so hooks stopped during a rollback see a context that may already be cancelled or past its deadline.
- **`fx.RecoverFromPanics` covers functions given to `Provide`, `Decorate` and `Invoke`,** not lifecycle hooks, and is rejected inside an `fx.Module`.
- **cobra stores flag-group annotations on the `*pflag.Flag`** (`flag_groups.go:73`), and subcommands inherit the root's persistent flags as the same flag, so a mutually exclusive group set on the root applies to every command.
- **pflag v1.0.9 has `TextVar` and `GetText`,** so `--format` parses into `log.Format` and is read back without string handling. Its `TextVar` reports the empty name of a pointer type as the flag's type (`text.go:47`), so help shows no value placeholder unless the usage backquotes one.
- **fx runs a module's invokes before its parent's own,** each child in the order it was listed (`module.go:313`), so a root-level invoke runs after every module's.
- **Once slog's default is replaced, the log package writes into its handler at slog's bridge level,** Info unless `slog.SetLogLoggerLevel` says otherwise.
- **cobra prints a returned error unless the executing command sets `SilenceErrors`,** which `cli`'s quiet-exit example already relies on.
- **`log.NewEventLogger` logs fx's failures at Error in every mode,** with the full error chain.

## Goals / Non-Goals

**Goals:**

- A runner contract every loop module can import without pulling in cobra, viper or log.
- One way an app ends: a runner returns. Whatever ends it, the stop hooks run.
- Output an operator can trust: every line in the chosen format, nothing reported twice, exit codes intact.
- An app's `main` and commands that read as composition, not plumbing.

**Non-Goals:**

- Readiness and health endpoints; a healthcheck module can build on "a finished `Start` means ready".
- Restarting a runner, or supervising runners in any way beyond the first return stopping the rest.
- A test double; `testing/kernelruntest` waits for its first consumer.
- Static log attributes (service, version) or a runner-name attribute on records. Either is a middleware away, later.

## Decisions

### 1. Two modules, and the direction of their dependencies (→ docs/kernel.md)

```
kernelrun   fx, dig             runners, the lifecycle, kernelrun/runerr
   ▲   ▲
   │   └──── loop modules (httpserver, a receiver, …) register through kernelrun.ProvideRunner
   │
kernel      cobra, cli, log, log/tint, config/configloader, kernelrun
   ▲                            flags, commands, wiring
  app
```

- **Rejected: one module, as in go-modules.** Every loop module would import cobra, viper and log to register a runner, against the rule that importing one module never drags in another's dependencies.
- **Rejected: `kernel/kernel`, `kernel/kernelrun` as nested modules under a group directory.** The import path stutters, and the group-naming rule has no case for an empty name. Two top-level modules sharing a prefix follows go-modules' `turnstile`, `turnstilecontext` and friends.
- **Rejected: `runerr` as a module of its own.** Its readers are OnStop hooks, which already depend on fx, and the lifecycle that writes it lives in `kernelrun`. As the package `kernelrun/runerr` it costs no module or release, imports only `context`, and keeps `runerr.From`.
- **Rejected: a `kernelrun` without the lifecycle.** With it, a loop module tests its loop through the real start, run and stop, and an app that doesn't use cobra still has a lifecycle. `kernel` becomes wiring alone.

### 2. kernelrun's shape

```go
package kernelrun

type Func func(ctx context.Context) error

type Runner struct {
	Name string
	Func Func
}

const RunnerGroup = `group:"kernelrun:runner"`

func ProvideRunner(name string, constructor any) fx.Option
func WithRunner(name string, f Func) fx.Option

const (
	DefaultStartTimeout = 15 * time.Second
	DefaultStopTimeout  = 15 * time.Second
)

type Options struct {
	StartTimeout time.Duration // zero means DefaultStartTimeout
	StopTimeout  time.Duration // zero means DefaultStopTimeout
}

func Run(ctx context.Context, o Options, opts ...fx.Option) error

var (
	ErrNoRunners       = errors.New("no runners")
	ErrDuplicateRunner = errors.New("duplicate runner name")
	ErrInvalidRunner   = errors.New("invalid runner")
)
```

```go
package runerr

var ErrNotStarted = errors.New("the app didn't start")

func With(ctx context.Context, err error) context.Context
func From(ctx context.Context) error
```

- **`Func`, not `RunFunc`, and `runerr.From`, not `runerr.ErrorFrom`:** a name doesn't repeat its package.
- **`Runner` is exported,** so the group's element type is visible and a hand-built `fx.Annotate` can contribute one, as `log.OrderedMiddleware` is.
- **Two registration helpers,** for the reason `log` has two: fx can't tell a function value from a constructor. `ProvideRunner` reuses `log.ProvideMiddleware`'s private `fx.Module`: the constructor's `Func` is provided privately under a fixed name, and an adapter taking it through a params struct pairs it with the name into the group. There's no exported name-tag helper like go-modules' `RunFuncNameTag`.
- **The timeouts are a plain `Options` struct, not fx values:** they bound the app's own start and stop, so they're needed outside its graph. Zero means the `Default*` constant, through an unexported resolver. The defaults are fx's own, 15 seconds each; go-modules stopped after 10, and matching fx leaves one less number to explain.
- **No `Module` variable.** `Run` builds the app rather than joining it, so there's nothing to list; the constitution's `Module` rule gets the exception, alongside `config/configloader`.

### 3. How Run runs an app (→ docs/kernel.md)

```
fx.New(RecoverFromPanics, opts…, collect runners and the optional logger)
  │  failure ─▶ "building the app: <root cause>"
check runners ─▶ ErrNoRunners · ErrDuplicateRunner · ErrInvalidRunner
  │
Start(ctx + start timeout, runerr.With(ErrNotStarted))
  │  failure that is ctx's own ending ─▶ nil (clean)        any other failure ─▶ "starting the app: <root cause>"
runners, each in a goroutine, on a context cancelled when the first returns
  │  first return ─▶ cancel the rest ─▶ wait for all
Stop(WithoutCancel(ctx) + stop timeout, runerr.With(outcome))
  │
return the joined crash and stop errors, or nil
```

- **The checks run after `fx.New` and before `Start`,** so an invalid set of runners fails before any hook starts. An empty name or a nil `Func` is `ErrInvalidRunner`, a repeated name is `ErrDuplicateRunner` (names identify runners in errors and logs), and none at all is `ErrNoRunners`. Even a `config check` command registers a runner, one that returns nil once the config has loaded.
- **The first return stops everything.** errgroup cancels only on an error, so a server that exits cleanly but unexpectedly would leave the rest running. Here the first runner to return, whatever it returns, cancels its siblings' context with a cause naming it, and `Run` waits for all of them. A runner that ignores cancellation is left to the caller's second signal, which `cli` turns into the operating system's default action.
- **Clean or crash, per runner:**
  - nil is clean;
  - the runners' context's own error is clean once that context is done, whether the first return or the caller cancelled it or the caller's deadline passed (`errors.Is(err, runCtx.Err())`);
  - anything else is a crash, wrapped as `running <name>: %w`, including that same error returned while the context is still live.

  go-modules treated any `context.Canceled` as clean, which hid a runner that returned an internal cancellation while the app was healthy. The first implementation here matched only `context.Canceled`, so a runner returning `ctx.Err()` at the caller's deadline was a crash. Review settled that a deadline on the caller's context is a planned stop: the caller chose it, and can check `ctx.Err()` after `Run` if it cares.
- **A runner's panic is recovered** into a crash, `running <name>: panic: <value>`, with the stack logged at Error. The app then stops like any crash, so OnStop hooks still drain and flush. The process still fails, after a clean shutdown.
- **A runner that exits through `runtime.Goexit` is a crash too,** `running <name>: exited without returning`. Classification and the cancellation of the others run in a `defer`, so a `Goexit` (a stray `t.FailNow` in a test, say) still stops the run instead of leaving the rest running.
- **A startup error is its root cause** (`dig.RootCause`), prefixed `building the app:` for a constructor or invoke and `starting the app:` for an OnStart hook, so a missing dependency reads differently from a hook failure. go-modules kept the full chain under `--verbose`; here `log.NewEventLogger` already logs the full chain at Error in every mode, so the returned error doesn't depend on a flag.
- **A start cut short by the caller's context is clean.** If `Start` fails with the caller's context's own error, cancelled or past its deadline, `Run` returns nil, matching the run phase's rule. Any other start failure is returned, even if the context has ended since; the first implementation swallowed every start failure once the context was done. fx's own `withTimeout` returns the context's error whenever the context is done as a hook returns, so a hook failure exactly concurrent with a signal reads as the signal; the check closes the window after fx returns.
- **A failed start's rollback reads `runerr.ErrNotStarted`.** fx rolls back with the start context, so `Run` attaches the sentinel to it from the beginning. OnStop hooks only see that context during a rollback, so a hook taking the clean path only on nil never mistakes a failed start for a clean run. OnStart hooks could read it too, but have no reason to. The rollback context may still be cancelled or past its deadline, which is harmless when loop modules follow the lifecycle rule (decision 8), since nothing serves before the runners start.
- **The stop context is `context.WithoutCancel(ctx)` with the stop timeout,** so OnStop hooks keep the run context's values (trace and request scope, say) without its cancellation. go-modules used `context.Background()`. `runerr.With` attaches the joined crash errors, or nil on a clean exit.
- **`Run` returns `errors.Join` of the crash errors and `stopping the app: %w`.** Errors keep their chains, so a runner returning `cli.Exit(3, err)` still exits 3 through `cli.Execute`.

### 4. Signals and fx.Shutdowner (→ docs/kernel.md)

`Run` never calls `app.Done`, `app.Wait` or `app.Run`, so it never subscribes to SIGINT or SIGTERM. The caller's context is the only way a signal reaches it, and `cli`'s second Ctrl-C keeps its default action.

The cost is that `fx.Shutdowner` goes unheard. Rather than wire a second way to end the app, the constitution bans it with forbidigo (`fx.Shutdowner`, "return from a runner instead"). A runner returning already ends the app, and it carries an error when there is one.

### 5. Logging in kernelrun

`Run` takes the graph's `*slog.Logger` through an optional parameter, and logs nothing when there isn't one. It never imports `log`.

- Run and stop failures are logged at Error, each with its runner's name, and a runner's panic with its stack.
- The lifecycle is logged at Debug: the runners starting, the first one returning and why, and the shutdown's cause (`context.Cause`, which names `cli`'s signal).
- Startup failures aren't logged by `Run`. fx's event logger reports them when it's wired, and `kernel` wires it. A caller using `Run` alone reports the returned error itself.

### 6. kernel's shape

```go
package kernel

const (
	DebugFlag  = "debug"
	QuietFlag  = "quiet"
	FormatFlag = "format"
)

var ErrMissingFlag = errors.New("missing kernel flag")

type Options struct {
	Format       log.Format              // --format's default: FormatJSON for a service, FormatText for a tool
	TextHandler  log.TextHandlerProvider // nil means tint.New
	StartTimeout time.Duration           // zero means kernelrun's default
	StopTimeout  time.Duration
	Shared       fx.Option               // added to every command's app; nil means none
}

type App struct{ /* the Options */ }

func New(o Options) App

func (a App) RootCommand(name, short string) *cobra.Command
func (a App) Command(use, short string, opts ...fx.Option) *cobra.Command
func (a App) Run(cmd *cobra.Command, opts ...fx.Option) error
```

```go
var app = kernel.New(kernel.Options{
	Format: log.FormatText,
	Shared: fx.Options(configloader.Provide(configloader.Options[Config]{Name: "mytool"})),
})

root := app.RootCommand("mytool", "Does my thing")
root.AddCommand(
	cli.NewVersionCommand(),
	app.Command("check", "Check the config", kernelrun.WithRunner("check", func(context.Context) error { return nil })),
	&cobra.Command{
		Use:  "import <file>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.Run(cmd, fx.Supply(ImportPath(args[0])), importer.ProvideRunner())
		},
	},
)
```

- **`App` is a small immutable value,** so it's returned and received by value. It holds developer settings, not fx values, because the flag defaults are needed before any graph exists.
- **`New` panics on an invalid `Options.Format`,** with an error matching `log.ErrUnknownFormat`. pflag swallows the default's `MarshalText` error, so an invalid default would otherwise show as `(default )` in help and fail every command with what reads like the operator's mistake. It's a fault only a code change can fix, so the program fails as it starts, as `regexp.MustCompile` does, rather than `New` returning an error every program has to handle.
- **`Run` is the primitive and `Command` a shortcut over it.** `Command` takes no arguments (`cobra.NoArgs`); a command with its own arguments or flags calls `Run` from its `RunE` and supplies them as fx values.
- **`RootCommand(name, short)` mirrors `cli.NewRootCommand`,** which it calls, then adds the three flags.
- **The flag names are constants,** as `cli.ConfigFlag` is.

### 7. What kernel's Run wires

```
fx.WithLogger(log.NewEventLogger)
fx.Module("kernel", fx.Invoke(slog.SetDefault))
log.Module · log.WithDebug · log.WithQuiet · log.WithFormat · log.WithTextHandler (Options.TextHandler, or tint.New)
configloader.WithFilename(--config)
fx.Populate(&logger)
Options.Shared
the command's opts
        ─▶ kernelrun.Run(cmd.Context(), kernelrun.Options{StartTimeout, StopTimeout}, …)
```

- **The flags are read from `cmd.Flags()`,** which includes the root's persistent flags. `--format` is read with `GetText` into a `log.Format`. A missing kernel flag fails with `ErrMissingFlag`, naming it: the root wasn't built by `RootCommand`, which is a wiring mistake that defaults would hide.
- **The flags.**
  - `--debug` has no shorthand, `--quiet` has `-q` and `--format` has none. `--config` keeps cli's no-shorthand stance.
  - `--debug` and `--quiet` are mutually exclusive through cobra, so `log`'s "debug wins" only matters for code wiring both by hand.
  - `--format` is a `TextVar` with `Options.Format` as its default, so `--format yaml` fails during flag parsing. Its usage backquotes a placeholder (``log `format`: …``), since pflag can't name a `TextVar`'s type, so help reads `--format format`.
- **The text handler is a setting, not fx wiring.** Supplying tint and letting an app override it through fx would mean providing the same tagged value twice, which fx rejects.
- **`configloader.WithFilename` is always supplied.** In an app that doesn't load config, nothing consumes it. The README says the kernel owns it, since an app supplying its own would collide.
- **The default logger is set inside the graph and restored outside it.** The invoke making the app's logger slog's default routes third-party `slog` and stdlib `log` output through it. `Run` saves the previous default and restores it with a `defer` after `kernelrun.Run` returns. An OnStop hook wouldn't run if construction failed after the invoke, and tests that run several apps would leak into each other.
- **The invoke sits in a module of its own, listed first.** fx runs every module's invokes before the root's own, so the first implementation's root-level `fx.Invoke(slog.SetDefault)` ran after the invokes of modules in `Options.Shared` and the command's options, and anything they logged through slog's default came out in the old format. A module listed ahead of the others runs its invoke first.
- **The log package writes at Warn while the app runs.** Once slog's default is replaced, the log package writes into its handler at slog's bridge level, Info by default, so `--quiet` dropped everything libraries report through it, net/http's ErrorLog included. The log package has no levels, and what goes through it is mostly error reports, so `Run` raises the bridge level to Warn with `slog.SetLogLoggerLevel` and restores it with the rest.
- **The app's logger is populated for the kernel's own reporting.** `kernelrun` leaves its runner checks' failures to its caller, and fx never sees them, so `Run` logs `ErrNoRunners`, `ErrDuplicateRunner` and `ErrInvalidRunner` at Error itself before silencing cobra (decision 9). Without that, a command with no runners exited 1 with nothing printed or logged.

### 8. The lifecycle rule for loop modules (→ docs/kernel.md)

The constitution's run-loop rule gains how a loop module splits its work:

- **Prepare in OnStart:** bind the listener, open the subscription. A setup failure is then a startup failure, reported before any runner starts, and a finished `Start` means everything is ready.
- **Serve in the runner:** the loop, until its context is cancelled.
- **Clean up in OnStop:** release what OnStart acquired, within the stop timeout.

The module's own `ProvideRunner` registers through `kernelrun.ProvideRunner` under its `DefaultRunnerName`.

### 9. How failures reach the operator (→ docs/kernel.md)

| Phase | Fails with | Reported by | cobra's `Error:` line |
|---|---|---|---|
| flags | a parse error | cobra (no logger exists yet) | printed |
| kernel's wiring | `ErrMissingFlag` | cobra | printed |
| building, starting | the root cause | fx's event logger, full chain, Error | silenced |
| runner checks | `kernelrun`'s check sentinels | `kernel`, Error | silenced |
| run, stop | crash and stop errors | `kernelrun`, Error, per runner | silenced |

`kernel.Run` sets `cmd.SilenceErrors` whenever `kernelrun.Run` returns an error, once the failure has been logged. Every line a service writes stays in its format: a JSON service doesn't end its stream with a plain-text line. Nothing is reported twice, and `cli.Execute` still takes the exit code from the error. If the logger itself can't be built, fx falls back to its console logger, which still prints the failure.

### 10. Dependencies

| Dependency | Where | Why |
|---|---|---|
| go.uber.org/fx | kernelrun, kernel | the constitution's integration layer |
| go.uber.org/dig | kernelrun | `dig.RootCause`; already in the tree through fx |
| spf13/cobra | kernel | the commands it builds; already required through `cli` |
| `cli`, `log`, `log/tint`, `config/configloader` | kernel | what it wires; tint has no dependencies, and configloader brings viper, which apps importing `kernel` accept |
| `kernelrun` | kernel | the lifecycle |
| testify | tests | the constitution's assertion library |

No errgroup: the first-return semantics need a cancel-on-any-return that errgroup doesn't offer, and a `sync.WaitGroup` with `context.WithCancelCause` is all it takes.

### 11. Testing

- **`kernelrun`:**
  - running, the first return, clean and crash outcomes, recovered panics, the checks, both timeouts, the stop context's values and outcome, a constructor's panic, and the root-cause message;
  - signal independence, in a re-run of the test binary that sends itself SIGINT, as `cli` tests its handler;
  - logging, through a recording handler.
- **`kernel`:** through `cli.Execute` with arguments, a recording destination supplied in `Shared`, and runners from `kernelrun.WithRunner`:
  - the flags reaching `log`, mutual exclusion, and `--format` rejecting a bad name;
  - the event logger, `--config` reaching the loader, and the default logger set and restored;
  - silenced errors with exit codes intact (`cli.Exit(3, …)` exits 3), and a root not built by `RootCommand`.

## Risks / Trade-offs

- [A runner that ignores cancellation keeps `Run` waiting] → The caller's second signal ends the process; documented.
- [A panic in a lifecycle hook still crashes the process] → `RecoverFromPanics` covers only constructors, decorators and invokes, and fx runs the hooks. Hooks should do little and return errors.
- [A cancelled or timed-out start rolls back with a dead context] → Harmless under the lifecycle rule, since nothing serves before the runners, and the rolled-back hooks read `runerr.ErrNotStarted`. Documented for hooks that need time to release what OnStart took.
- [Run waits for runners without a bound, so a runner whose own shutdown is unbounded hangs the app] → The README's server example bounds its `Shutdown`, and the caller's second signal remains the way out.
- [`fx.Shutdowner` is unheard in apps outside this repo, where the lint ban doesn't apply] → The `kernelrun` README says so and points at runners.
- [Every app importing `kernel` builds viper, even one that never loads config] → Accepted: `cli` already gives every program `--config`, and the alternatives push the loader's wiring back onto every app.
- [Saving and restoring slog's default could trip sloglint's no-global check] → It's process setup, not logging. If sloglint reports it, a scoped `nolint` with the reason.

## Migration Plan

Two pull requests, in dependency order:

1. `kernelrun`, with the constitution edits and the `fx.Shutdowner` ban. Merging publishes `kernelrun/v0.1.0`.
2. `kernel`, once `kernelrun/v0.1.0` is published, since its go.mod requires it. Merging publishes `kernel/v0.1.0`.

go-modules is unaffected.

## Open Questions

- The flags' help texts. They can change in a patch.
