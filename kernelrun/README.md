# kernelrun

Runs an fx app's named runners between its start and stop: the first runner to return stops the rest, every ending shuts the app down through its OnStop hooks, and those hooks learn whether the run crashed. Loop modules import it to register their runners without pulling in cobra, viper or log.

## Configuration

None. The start and stop timeouts are developer settings, passed to `Run` in `Options`: they bound the app's own lifecycle, so they're needed before its graph exists.

## Exported Members

| Name | Description |
|------|-------------|
| Run | Builds an fx app from options, checks its runners, starts it, runs the runners until the first returns, stops it, and returns the crash and stop errors joined, or nil. |
| Options | Run's developer settings. The zero value uses the defaults. |
| Options.StartTimeout | Bounds the OnStart hooks. Zero means DefaultStartTimeout. |
| Options.StopTimeout | Bounds the OnStop hooks. Zero means DefaultStopTimeout. |
| DefaultStartTimeout | 15 seconds, fx's own default. |
| DefaultStopTimeout | 15 seconds, fx's own default. |
| Func | A runner's body. It runs until its work is done or its context is done. Returning nil, or its context's error once that context is done, is clean; anything else is a crash. |
| Runner | A named Func, the element type of RunnerGroup. |
| Runner.Name | Identifies the runner in errors and logs. Non-empty and unique within an app. |
| Runner.Func | The runner's body. Never nil. |
| RunnerGroup | The fx group tag Run collects Runners from. |
| WithRunner | Registers a Func value under a name. |
| ProvideRunner | Registers the Func a constructor builds from the graph under a name. The constructor's first result must have the Func type. |
| ErrNoRunners | Returned by Run, before any OnStart hook, for an app with no runners. |
| ErrDuplicateRunner | Returned by Run, before any OnStart hook, for two runners with one name. |
| ErrInvalidRunner | Returned by Run, before any OnStart hook, for a runner with an empty name or a nil Func. |
| ErrExited | The crash a runner that exits through runtime.Goexit, without returning, becomes. Run returns it wrapped with the runner's name. |
| runerr.With | Returns a copy of a context carrying an error as the run's outcome. For tests of OnStop hooks. |
| runerr.From | Returns the run's outcome from a context: nil after a clean run or when none is attached, the joined crash errors after a crash, and ErrNotStarted while fx rolls back a failed start. |
| runerr.ErrNotStarted | The outcome an OnStop hook reads while fx rolls back a start that failed, when no runner ran. |

## Usage

### Standalone

`Run` builds and runs the app itself, so a program that doesn't use the `kernel` module calls it from `main` with a context its signal handling cancels:

```go
import (
	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/kernelrun"
)

ctx, stop := cli.SignalContext(context.Background())
defer stop()

err := kernelrun.Run(ctx, kernelrun.Options{},
	fx.Supply(logger),
	kernelrun.WithRunner("ticker", func(ctx context.Context) error {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				logger.InfoContext(ctx, "tick")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}),
)
```

How a run goes:

- **Checked before start.** No runners, a repeated name, an empty name or a nil `Func` fails with `ErrNoRunners`, `ErrDuplicateRunner` or `ErrInvalidRunner` before any OnStart hook runs.
- **Started, then run.** The OnStart hooks run under the start timeout, then every runner starts in its own goroutine, on a context carrying the caller's values.
- **The first return stops the rest,** whatever it returns, and so does the caller's context ending. `Run` waits for every runner. One that ignores its context keeps `Run` waiting; `cli.SignalContext` hands the second Ctrl-C back to the operating system, which ends the process.
- **Clean or crash.** nil is clean, and so is the runner's context's own error once that context is done: cancelled by the first return or the caller, or past the caller's deadline. Anything else is a crash, `running <name>: <error>`. A panic is recovered into one, `running <name>: panic: <value>`, and a runner that exits through `runtime.Goexit` is one, `running <name>: exited without returning`, matching `ErrExited`. The app still stops through its OnStop hooks.
- **Stopped.** The OnStop hooks run under the stop timeout, on a context that keeps the caller's values without its cancellation.
- **Startup failures** are returned as their root cause, without fx's and dig's wrapping: `building the app: <error>` for a constructor or invoke, a panicking one included, and `starting the app: <error>` for an OnStart hook. A start that fails because the caller's context ended, cancelled or past its deadline, is a clean exit and `Run` returns nil. Any other start failure is returned, even if the context has ended since.
- **Errors keep their chains,** so a runner returning `cli.Exit(3, err)` still exits 3 through `cli.Execute`.

`Run` never subscribes to SIGINT or SIGTERM: the context is the only way a signal reaches the app.

When the graph holds a `*slog.Logger`, `Run` logs crashes (with a panic's stack) and stop failures at Error, and the lifecycle at Debug: each runner starting, the one that returned first, and the shutdown's cause. It never logs startup failures; route fx's events through `log.NewEventLogger` for those, or report the returned error.

### With fx

A loop module splits its work across the lifecycle. It prepares in OnStart, so a setup failure is a startup failure and a finished `Start` means ready; it serves in its runner until the runner's context is cancelled; and it releases in OnStop what OnStart acquired. Its own `ProvideRunner` registers through `kernelrun.ProvideRunner`:

```go
// runner.go in a server module.

const DefaultRunnerName = "httpserver"

func ProvideRunner() fx.Option {
	return kernelrun.ProvideRunner(DefaultRunnerName, func(s *Server) kernelrun.Func {
		return s.Serve
	})
}
```

```go
// Server binds its listener in an OnStart hook, serves on it here and closes it in an OnStop hook.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()

		// Bounded: Run waits for every runner before the stop timeout applies, so an unbounded Shutdown would wait
		// on a connection that never goes idle.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		_ = s.srv.Shutdown(shutdownCtx)
	}()

	if err := s.srv.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
```

A one-shot command is a runner that returns when its work is done, which stops the app:

```go
kernelrun.ProvideRunner("import", func(p ImportParams) kernelrun.Func {
	return func(ctx context.Context) error {
		return p.Importer.Import(ctx, p.Path)
	}
})
```

An OnStop hook reads the run's outcome with `runerr.From`, to flush differently after a crash, say. When fx rolls back a failed start, the hooks it stops read `runerr.ErrNotStarted`, so a hook taking the clean path only on nil never mistakes a failed start for a clean run. Tests hand a hook an outcome with `runerr.With`:

```go
lc.Append(fx.Hook{
	OnStop: func(ctx context.Context) error {
		if err := runerr.From(ctx); err != nil {
			return q.Nack(ctx)
		}

		return q.Ack(ctx)
	},
})
```

`fx.Shutdowner` is never heard: fx only delivers its requests to `app.Done` and `app.Wait`, which `Run` doesn't call. Return from a runner instead. This repo's lint config bans `fx.Shutdowner`.
