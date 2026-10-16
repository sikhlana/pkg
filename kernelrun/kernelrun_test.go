package kernelrun_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/kernelrun/runerr"
)

func TestRunStartsRunnersTogetherAfterStart(t *testing.T) {
	var started atomic.Bool

	aUp, bUp := make(chan struct{}), make(chan struct{})

	runner := func(up, other chan struct{}, afterStart, together *bool) kernelrun.Func {
		return func(context.Context) error {
			*afterStart = started.Load()

			close(up)

			select {
			case <-other:
				*together = true
			case <-time.After(5 * time.Second):
			}

			return nil
		}
	}

	var aAfter, bAfter, aTogether, bTogether bool

	err := run(t, kernelrun.Options{},
		hooks(func(context.Context) error {
			started.Store(true)

			return nil
		}, nil),
		kernelrun.WithRunner("a", runner(aUp, bUp, &aAfter, &aTogether)),
		kernelrun.WithRunner("b", runner(bUp, aUp, &bAfter, &bTogether)),
	)

	require.NoError(t, err)
	assert.True(t, aAfter, "a started before the OnStart hook finished")
	assert.True(t, bAfter, "b started before the OnStart hook finished")
	assert.True(t, aTogether && bTogether, "the runners weren't running at once")
}

func TestRunPassesTheCallersValues(t *testing.T) {
	ctx := context.WithValue(t.Context(), ctxKey{}, "value")

	var got any

	err := runContext(ctx, kernelrun.Options{}, kernelrun.WithRunner("api", func(ctx context.Context) error {
		got = ctx.Value(ctxKey{})

		return nil
	}))

	require.NoError(t, err)
	assert.Equal(t, "value", got)
}

func TestFirstReturnStopsTheRest(t *testing.T) {
	crash := errors.New("crashed")

	tests := []struct {
		name  string
		first kernelrun.Func
		want  error
	}{
		{name: "clean", first: returnNil},
		{name: "crash", first: func(context.Context) error { return crash }, want: crash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var waiterReturned atomic.Bool

			var stopped bool

			err := run(t, kernelrun.Options{},
				hooks(nil, func(context.Context) error {
					stopped = waiterReturned.Load()

					return nil
				}),
				kernelrun.WithRunner("first", tt.first),
				kernelrun.WithRunner("waiter", func(ctx context.Context) error {
					err := waitDone(ctx)

					waiterReturned.Store(true)

					return err
				}),
			)

			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}

			assert.True(t, stopped, "the app stopped before the waiter returned")
		})
	}
}

func TestCallerCancellationStopsEveryRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var returned atomic.Int32

	up := make(chan struct{}, 2)

	runner := func(ctx context.Context) error {
		up <- struct{}{}

		err := waitDone(ctx)

		returned.Add(1)

		return err
	}

	go func() {
		<-up
		<-up
		cancel()
	}()

	err := runContext(ctx, kernelrun.Options{}, kernelrun.WithRunner("a", runner), kernelrun.WithRunner("b", runner))

	require.NoError(t, err)
	assert.Equal(t, int32(2), returned.Load())
}

func TestOutcome(t *testing.T) {
	crash := errors.New("crashed")

	t.Run("everything clean", func(t *testing.T) {
		err := run(t, kernelrun.Options{},
			kernelrun.WithRunner("api", returnNil),
			kernelrun.WithRunner("worker", waitDone),
			kernelrun.WithRunner("poller", waitDone),
		)

		assert.NoError(t, err)
	})

	t.Run("a crash", func(t *testing.T) {
		err := run(t, kernelrun.Options{},
			kernelrun.WithRunner("api", func(context.Context) error { return crash }),
			kernelrun.WithRunner("worker", waitDone),
		)

		require.ErrorIs(t, err, crash)
		assert.EqualError(t, err, "running api: crashed")
	})

	t.Run("an early cancellation error", func(t *testing.T) {
		err := run(t, kernelrun.Options{},
			kernelrun.WithRunner("api", func(context.Context) error { return context.Canceled }),
			kernelrun.WithRunner("worker", waitDone),
		)

		require.ErrorIs(t, err, context.Canceled)
		assert.EqualError(t, err, "running api: context canceled")
	})

	t.Run("an exit code survives", func(t *testing.T) {
		err := run(t, kernelrun.Options{}, kernelrun.WithRunner("api", func(context.Context) error {
			return &codeError{code: 3}
		}))

		got, ok := errors.AsType[*codeError](err)
		require.True(t, ok, "err is %v", err)
		assert.Equal(t, 3, got.code)
	})
}

func TestPanickingRunner(t *testing.T) {
	logger, rec := withRecorder()

	var waiterCancelled, stopped bool

	err := run(t, kernelrun.Options{},
		logger,
		hooks(nil, func(context.Context) error {
			stopped = true

			return nil
		}),
		kernelrun.WithRunner("worker", func(context.Context) error { panic("boom") }),
		kernelrun.WithRunner("other", func(ctx context.Context) error {
			err := waitDone(ctx)
			waiterCancelled = true

			return err
		}),
	)

	require.EqualError(t, err, "running worker: panic: boom")
	assert.True(t, waiterCancelled)
	assert.True(t, stopped)

	crashed, ok := rec.find("runner crashed")
	require.True(t, ok)
	assert.Equal(t, "worker", crashed.Attrs["runner"])
	assert.Contains(t, crashed.Attrs["stack"], "kernelrun_test.TestPanickingRunner")
}

func TestChecksBeforeStart(t *testing.T) {
	tests := []struct {
		name    string
		runners []fx.Option
		want    error
		message string
	}{
		{
			name:    "no runners",
			want:    kernelrun.ErrNoRunners,
			message: "checking runners: no runners",
		},
		{
			name:    "a duplicate name",
			runners: []fx.Option{kernelrun.WithRunner("api", returnNil), kernelrun.WithRunner("api", returnNil)},
			want:    kernelrun.ErrDuplicateRunner,
			message: `checking runners: duplicate runner name "api"`,
		},
		{
			name:    "an empty name",
			runners: []fx.Option{kernelrun.WithRunner("", returnNil)},
			want:    kernelrun.ErrInvalidRunner,
			message: "checking runners: invalid runner: empty name",
		},
		{
			name:    "no function",
			runners: []fx.Option{kernelrun.WithRunner("api", nil)},
			want:    kernelrun.ErrInvalidRunner,
			message: `checking runners: invalid runner "api": no function`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started bool

			opts := append([]fx.Option{hooks(func(context.Context) error {
				started = true

				return nil
			}, nil)}, tt.runners...)

			err := run(t, kernelrun.Options{}, opts...)

			require.ErrorIs(t, err, tt.want)
			require.EqualError(t, err, tt.message)
			assert.False(t, started, "an OnStart hook ran")
		})
	}
}

func TestConstructorErrorIsTheRootCause(t *testing.T) {
	type config struct{}

	err := run(t, kernelrun.Options{},
		fx.Provide(func() (config, error) { return config{}, errors.New("validating config: port required") }),
		kernelrun.ProvideRunner("api", func(config) kernelrun.Func { return returnNil }),
	)

	require.EqualError(t, err, "starting the app: validating config: port required")
}

func TestRecoverFromPanicsTurnsAConstructorPanicIntoAnError(t *testing.T) {
	type config struct{}

	err := run(t, kernelrun.Options{},
		fx.Provide(func() config { panic("boom") }),
		kernelrun.ProvideRunner("api", func(config) kernelrun.Func { return returnNil }),
	)

	require.ErrorContains(t, err, "starting the app: ")
	assert.ErrorContains(t, err, "boom")
}

func TestStartTimeout(t *testing.T) {
	var ran bool

	begin := time.Now()

	err := run(t, kernelrun.Options{StartTimeout: 50 * time.Millisecond},
		hooks(waitDone, nil),
		kernelrun.WithRunner("api", func(context.Context) error {
			ran = true

			return nil
		}),
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.GreaterOrEqual(t, time.Since(begin), 50*time.Millisecond)
	assert.False(t, ran, "a runner started")
}

func TestCancelledDuringStartIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var ran bool

	err := runContext(ctx, kernelrun.Options{},
		hooks(func(hookCtx context.Context) error {
			cancel()

			return waitDone(hookCtx)
		}, nil),
		kernelrun.WithRunner("api", func(context.Context) error {
			ran = true

			return nil
		}),
	)

	require.NoError(t, err)
	assert.False(t, ran, "a runner started")
}

func TestStopContextOutcome(t *testing.T) {
	crash := errors.New("crashed")

	tests := []struct {
		name   string
		runner kernelrun.Func
		want   error
	}{
		{name: "clean", runner: returnNil},
		{name: "crash", runner: func(context.Context) error { return crash }, want: crash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var outcome error

			_ = run(t, kernelrun.Options{},
				hooks(nil, func(ctx context.Context) error {
					outcome = runerr.From(ctx)

					return nil
				}),
				kernelrun.WithRunner("api", tt.runner),
			)

			if tt.want == nil {
				assert.NoError(t, outcome)
			} else {
				assert.ErrorIs(t, outcome, tt.want)
			}
		})
	}
}

func TestStopContextAfterTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "value"))
	defer cancel()

	var (
		value    any
		live     bool
		deadline time.Time
	)

	err := runContext(ctx, kernelrun.Options{StopTimeout: time.Hour},
		hooks(nil, func(ctx context.Context) error {
			value = ctx.Value(ctxKey{})
			live = ctx.Err() == nil
			deadline, _ = ctx.Deadline()

			return nil
		}),
		kernelrun.WithRunner("api", func(runCtx context.Context) error {
			cancel()

			return waitDone(runCtx)
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, "value", value)
	assert.True(t, live, "the stop context was done")
	assert.WithinDuration(t, time.Now().Add(time.Hour), deadline, time.Minute)
}

func TestFailingOnStopJoinedWithACrash(t *testing.T) {
	crash := errors.New("crashed")
	stopFailure := errors.New("flushing")

	err := run(t, kernelrun.Options{},
		hooks(nil, func(context.Context) error { return stopFailure }),
		kernelrun.WithRunner("api", func(context.Context) error { return crash }),
	)

	require.ErrorIs(t, err, crash)
	require.ErrorIs(t, err, stopFailure)
}

func TestLogsACrash(t *testing.T) {
	logger, rec := withRecorder()

	_ = run(t, kernelrun.Options{},
		logger,
		kernelrun.WithRunner("api", func(context.Context) error { return errors.New("crashed") }),
	)

	crashed, ok := rec.find("runner crashed")
	require.True(t, ok)
	assert.Equal(t, "ERROR", crashed.Level.String())
	assert.Equal(t, "api", crashed.Attrs["runner"])
	assert.Equal(t, "crashed", crashed.Attrs["error"])
}

func TestLogsAStopFailure(t *testing.T) {
	logger, rec := withRecorder()

	_ = run(t, kernelrun.Options{},
		logger,
		hooks(nil, func(context.Context) error { return errors.New("flushing") }),
		kernelrun.WithRunner("api", returnNil),
	)

	failed, ok := rec.find("stopping the app failed")
	require.True(t, ok)
	assert.Equal(t, "ERROR", failed.Level.String())
	assert.Contains(t, failed.Attrs["error"], "flushing")
}

func TestLogsTheLifecycle(t *testing.T) {
	logger, rec := withRecorder()

	err := run(t, kernelrun.Options{}, logger, kernelrun.WithRunner("api", returnNil))

	require.NoError(t, err)

	var messages []string

	for _, r := range rec.all() {
		assert.Equal(t, "DEBUG", r.Level.String(), "record %q", r.Message)

		messages = append(messages, r.Message)
	}

	assert.Equal(t, []string{"starting runner", "runner returned first", "stopping the app", "app stopped"}, messages)

	first, _ := rec.find("runner returned first")
	assert.Equal(t, "api", first.Attrs["runner"])

	stopping, _ := rec.find("stopping the app")
	assert.Equal(t, "runner api returned: context canceled", stopping.Attrs["cause"])
}

func TestNoLoggerNoPanic(t *testing.T) {
	crash := errors.New("crashed")

	err := run(t, kernelrun.Options{}, kernelrun.WithRunner("api", func(context.Context) error { return crash }))

	assert.ErrorIs(t, err, crash)
}
