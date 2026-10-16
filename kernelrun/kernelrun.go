package kernelrun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/dig"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun/runerr"
)

// DefaultStartTimeout bounds the app's OnStart hooks when Options.StartTimeout is zero.
const DefaultStartTimeout = 15 * time.Second

// DefaultStopTimeout bounds the app's OnStop hooks when Options.StopTimeout is zero.
const DefaultStopTimeout = 15 * time.Second

var (
	// ErrNoRunners is returned by Run for an app that registers no runners.
	ErrNoRunners = errors.New("no runners")

	// ErrDuplicateRunner is returned by Run for an app that registers two runners under one name.
	ErrDuplicateRunner = errors.New("duplicate runner name")

	// ErrInvalidRunner is returned by Run for a runner with an empty name or a nil Func.
	ErrInvalidRunner = errors.New("invalid runner")
)

// Options holds Run's developer settings. The zero value uses the defaults.
type Options struct {
	// StartTimeout bounds the OnStart hooks. Zero means DefaultStartTimeout.
	StartTimeout time.Duration

	// StopTimeout bounds the OnStop hooks. Zero means DefaultStopTimeout.
	StopTimeout time.Duration
}

// runParams is what Run collects from the app's graph.
type runParams struct {
	fx.In

	Runners []Runner     `group:"kernelrun:runner"`
	Logger  *slog.Logger `optional:"true"`
}

// errExited is the crash a runner that exits through runtime.Goexit, without returning, becomes.
var errExited = errors.New("exited without returning")

// panicError is the crash a runner's recovered panic becomes.
type panicError struct {
	value any
	stack []byte
}

// Run builds an fx app from opts, starts it, runs its runners until the first one returns, and stops it.
//
// Before starting the app, Run checks its runners: none fails with ErrNoRunners, a repeated name with
// ErrDuplicateRunner, and an empty name or a nil Func with ErrInvalidRunner. A failure to build the app or to start it
// is returned as its root cause, without fx's and dig's wrapping, prefixed "building the app" or "starting the app"; a
// panicking constructor or invoke is a build failure. A start that fails because ctx ended, cancelled or past its
// deadline, is a clean exit and Run returns nil; any other start failure is returned, even if ctx has ended since.
// OnStop hooks that fx runs to roll back a failed start read runerr.ErrNotStarted from runerr.From.
//
// Each runner runs in its own goroutine on a context that carries ctx's values. When the first one returns, for any
// reason, or ctx ends, the others' context is cancelled, and Run waits for all of them. A runner that ignores its
// context keeps Run waiting. A runner returning nil is clean, and so is one returning its context's error once that
// context is done, whether the first return, a cancelled ctx or ctx's deadline ended it. Any other error, a recovered
// panic, or an exit through runtime.Goexit is a crash, wrapped with the runner's name.
//
// The OnStop hooks then run on a context that keeps ctx's values without its cancellation, bounded by the stop
// timeout, and carrying the joined crash errors, or nil, for runerr.From. Run returns the crash errors joined with any
// stop failure, or nil.
//
// Run never subscribes to operating-system signals and never hears fx.Shutdowner; ctx is the only way to stop the app
// from outside its runners. When the graph holds a *slog.Logger, Run logs crashes and stop failures through it at
// Error and the lifecycle at Debug. Startup failures are left to fx's event logger and the caller.
func Run(ctx context.Context, o Options, opts ...fx.Option) error {
	var p runParams

	app := fx.New(
		fx.RecoverFromPanics(),
		fx.Options(opts...),
		fx.Invoke(func(in runParams) { p = in }),
	)

	if err := app.Err(); err != nil {
		return fmt.Errorf("building the app: %w", dig.RootCause(err))
	}

	if err := check(p.Runners); err != nil {
		return fmt.Errorf("checking runners: %w", err)
	}

	logger := cmp.Or(p.Logger, slog.New(slog.DiscardHandler))

	if err := start(ctx, app, o.startTimeout()); err != nil {
		if endedBy(ctx, err) {
			return nil
		}

		return fmt.Errorf("starting the app: %w", dig.RootCause(err))
	}

	outcome := errors.Join(runAll(ctx, logger, p.Runners)...)

	return errors.Join(outcome, stop(ctx, app, logger, outcome, o.stopTimeout()))
}

func (o Options) startTimeout() time.Duration {
	return cmp.Or(o.StartTimeout, DefaultStartTimeout)
}

func (o Options) stopTimeout() time.Duration {
	return cmp.Or(o.StopTimeout, DefaultStopTimeout)
}

// Error returns "panic: " followed by the panic's value.
func (e *panicError) Error() string {
	return fmt.Sprintf("panic: %v", e.value)
}

// check reports the first problem with runners.
func check(runners []Runner) error {
	if len(runners) == 0 {
		return ErrNoRunners
	}

	seen := make(map[string]bool, len(runners))

	for _, r := range runners {
		switch {
		case r.Name == "":
			return fmt.Errorf("%w: empty name", ErrInvalidRunner)
		case r.Func == nil:
			return fmt.Errorf("%w %q: no function", ErrInvalidRunner, r.Name)
		case seen[r.Name]:
			return fmt.Errorf("%w %q", ErrDuplicateRunner, r.Name)
		}

		seen[r.Name] = true
	}

	return nil
}

// start runs the app's OnStart hooks under timeout. Their context carries runerr.ErrNotStarted, which only OnStop
// hooks read, and only when fx rolls back a failed start on this context.
func start(ctx context.Context, app *fx.App, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(runerr.With(ctx, runerr.ErrNotStarted), timeout)
	defer cancel()

	return app.Start(ctx)
}

// runAll runs every runner until the first returns, cancels the rest, waits for them and returns their crash errors,
// with a nil for each clean runner.
func runAll(ctx context.Context, logger *slog.Logger, runners []Runner) []error {
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	crashes := make([]error, len(runners))

	var (
		first sync.Once
		wg    sync.WaitGroup
	)

	for i, r := range runners {
		logger.LogAttrs(ctx, slog.LevelDebug, "starting runner", slog.String("runner", r.Name))

		wg.Go(func() {
			var (
				err      error
				returned bool
			)

			// Deferred, so a runner that exits through runtime.Goexit still ends the run, as a crash.
			defer func() {
				if !returned {
					err = errExited
				}

				// Classified before the cancellation below, so a first runner returning context.Canceled of its own
				// accord is a crash.
				crashes[i] = classify(runCtx, logger, r.Name, err)

				first.Do(func() {
					attrs := []slog.Attr{slog.String("runner", r.Name)}

					if err != nil {
						attrs = append(attrs, slog.Any("error", err))
					}

					logger.LogAttrs(ctx, slog.LevelDebug, "runner returned first", attrs...)
					cancel(fmt.Errorf("runner %s returned: %w", r.Name, context.Canceled))
				})
			}()

			err = call(runCtx, r.Func)
			returned = true
		})
	}

	wg.Wait()

	logger.LogAttrs(ctx, slog.LevelDebug, "stopping the app", slog.String("cause", context.Cause(runCtx).Error()))

	return crashes
}

// call runs f, turning a panic into a *panicError.
func call(ctx context.Context, f Func) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &panicError{value: v, stack: debug.Stack()}
		}
	}()

	return f(ctx)
}

// classify returns nil for a clean return and the crash error otherwise, logging the crash.
func classify(runCtx context.Context, logger *slog.Logger, name string, err error) error {
	if clean(runCtx, err) {
		return nil
	}

	attrs := []slog.Attr{slog.String("runner", name), slog.Any("error", err)}

	if p, ok := errors.AsType[*panicError](err); ok {
		attrs = append(attrs, slog.String("stack", string(p.stack)))
	}

	logger.LogAttrs(runCtx, slog.LevelError, "runner crashed", attrs...)

	return fmt.Errorf("running %s: %w", name, err)
}

// clean reports whether a runner returning err on runCtx exited cleanly: with nil, or with runCtx's own error once
// runCtx is done.
func clean(runCtx context.Context, err error) bool {
	return err == nil || endedBy(runCtx, err)
}

// endedBy reports whether ctx is done and err is its error, so that ctx ending, cancelled or past its deadline, is
// what err reports rather than a failure of its own.
func endedBy(ctx context.Context, err error) bool {
	return ctx.Err() != nil && errors.Is(err, ctx.Err())
}

// stop runs the app's OnStop hooks under timeout, on a context that keeps ctx's values without its cancellation and
// carries outcome.
func stop(ctx context.Context, app *fx.App, logger *slog.Logger, outcome error, timeout time.Duration) error {
	stopCtx, cancel := context.WithTimeout(runerr.With(context.WithoutCancel(ctx), outcome), timeout)
	defer cancel()

	if err := app.Stop(stopCtx); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "stopping the app failed", slog.Any("error", err))

		return fmt.Errorf("stopping the app: %w", err)
	}

	logger.LogAttrs(ctx, slog.LevelDebug, "app stopped")

	return nil
}
