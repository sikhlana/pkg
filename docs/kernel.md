# The kernel

[kernelrun/README.md](../kernelrun/README.md) and [kernel/README.md](../kernel/README.md) say how to register runners, run an app and build a program's commands. This page says why they're shaped that way. The full discussion, including the alternatives rejected, is in the archived [add-kernel](../openspec/changes/archive/2026-10-17-add-kernel/design.md) change.

## Two modules, so loop modules stay small

```
kernelrun   fx, dig             runners, the lifecycle, kernelrun/runerr
   ▲   ▲
   │   └──── loop modules register through kernelrun.ProvideRunner
   │
kernel      cobra, cli, log, log/tint, config/configloader, kernelrun
   ▲
  app
```

Every server and receiver registers a runner, so whatever holds the runner contract is imported by all of them. go-modules kept it in the same module as everything else the kernel did, so one import would have pulled cobra, viper and log into every loop module. Here the contract and the lifecycle live in `kernelrun`, which needs only fx. The wiring of flags, logging and config lives in `kernel`, which only apps import, and which changes whenever `cli`, `log` or `configloader` do.

The run's outcome, `kernelrun/runerr`, is a package inside `kernelrun` rather than a module of its own. Its readers are OnStop hooks, which already depend on fx, and the lifecycle that writes it lives beside it.

## One way to end an app: a runner returns

`Run` starts every runner once the OnStart hooks have finished. The first runner to return, whatever it returns, cancels the others, and `Run` waits for all of them before stopping the app. errgroup stops only on an error, so a server that exited cleanly but unexpectedly would have left the rest running. A runner that ignores cancellation is left to the caller's second signal.

`fx.Shutdowner` is banned instead of supported. fx delivers its requests only to `app.Done` and `app.Wait`, and calling either subscribes fx to SIGINT and SIGTERM, which would take the second Ctrl-C away from `cli`. A runner returning already ends the app, with an error when there is one.

## Clean or crash

An ending is clean only when it reports the context's own ending: nil, or the runner's context error once that context is done, whether the first return cancelled it, the caller cancelled it, or the caller's deadline passed. The caller chose the deadline, so reaching it is a planned stop. The same error returned while the context is live is a crash, which go-modules hid by treating every `context.Canceled` as clean.

The same rule judges a failed start: a start cut short by the caller's context ending is clean, and any other start failure is reported, even if the context has ended since.

Every crash still stops the app through its OnStop hooks. A panic is recovered into a crash, and so is a runner that exits through `runtime.Goexit`. The process fails, but only after it has shut down cleanly.

## What OnStop hooks see

The OnStop hooks run on a context that keeps the caller's values without its cancellation, so a signal doesn't cut their cleanup short, bounded by the stop timeout. It carries the run's outcome for `runerr.From`: nil after a clean run, the crash errors after a crash.

fx rolls back a failed start with the start context, not the stop context. That context carries `runerr.ErrNotStarted`, so a hook taking its clean path only on nil never mistakes a failed start for a clean run.

## The lifecycle rule for loop modules

A loop module prepares in OnStart (binding its listener, opening its subscription), serves in its runner until the runner's context ends, and releases in OnStop what OnStart acquired. A setup failure is then a startup failure, reported before any runner starts, and a finished `Start` means everything is ready. It also makes a cancelled start harmless: fx rolls back with a context that may already be done, but nothing is serving yet.

## Every failure reported once, in the log's format

A JSON service shouldn't end its log stream with a plain-text line, and a failure shouldn't be reported twice. Each phase therefore has one reporter:

| Phase | Reported by |
|---|---|
| flags, and a root without the kernel's flags | cobra, since no logger exists yet |
| building and starting the app | fx's event logger, with the full error chain, at Error |
| `kernelrun`'s checks on the runners | `kernel`, at Error |
| running and stopping | `kernelrun`, at Error, per runner |

Once the app has been built, `kernel` silences cobra's own printing of the error but still returns it, so the exit code a runner chose (`cli.Exit(3, err)`) reaches `cli.Execute`. Because the event logger logs the full chain, the returned startup error is always just its root cause, without the dependency-injection layers around it.

## slog's default while an app runs

Third-party code logs through slog's default logger or the standard `log` package, so `kernel` makes the app's logger slog's default while the app runs. Three details make that work:

- **It's set from a module listed first.** fx runs every module's invokes before the root's own, so a root-level invoke would have let other modules' invokes log in the old format.
- **The `log` package writes at Warn.** It has no levels, and what libraries write through it is mostly error reports, so they survive `--quiet`.
- **Everything is restored outside the graph, when the command returns.** That covers slog's default, the `log` package's output and flags, and its level. An OnStop hook wouldn't run if construction failed, and tests that run several apps would leak into each other.
