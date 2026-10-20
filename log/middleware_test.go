package log_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

// dropping is a middleware that drops every record.
func dropping(context.Context, slog.Record, func(context.Context, slog.Record) error) error {
	return nil
}

// pointer is a middleware with a pointer receiver, so a nil *pointer still implements log.Middleware. It drops every
// record, so one that isn't left out shows.
type pointer struct{}

func (*pointer) Handle(context.Context, slog.Record, func(context.Context, slog.Record) error) error {
	return nil
}

// noop is a comparable middleware that passes every record on.
type noop struct{}

func (noop) Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	return next(ctx, r)
}

// newModuleLogger runs log.Module with opts over a recording, and returns the logger and the recording. The app stops
// when the test ends.
func newModuleLogger(t *testing.T, opts ...fx.Option) (*slog.Logger, *recording) {
	t.Helper()

	var logger *slog.Logger

	rec := &recording{t: t}

	app := fxtest.New(t, append([]fx.Option{
		log.Module,
		log.WithFormat(log.FormatText),
		log.WithTextHandler(rec.provide),
		fx.Populate(&logger),
	}, opts...)...)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	return logger, rec
}

func TestMiddlewareSeesTheContext(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.Middleware{log.MiddlewareFunc(addRequestID)},
	})

	logger.InfoContext(withRequestID(t, "r1"), "served")

	assert.Equal(t, []string{"[request_id=r1]"}, rec.attrs())
}

func TestMiddlewareDropsARecord(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.Middleware{log.MiddlewareFunc(dropping)},
	})

	logger.InfoContext(t.Context(), "dropped")

	assert.Empty(t, rec.all())
}

func TestMiddlewareOrder(t *testing.T) {
	t.Run("a higher order sees a lower one's attribute", func(t *testing.T) {
		var seen []string

		logger, _ := newRecorded(t, log.Params{
			Middleware: []log.Middleware{
				declared{
					MiddlewareFunc: func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
						r.Attrs(func(a slog.Attr) bool {
							seen = append(seen, a.String())

							return true
						})

						return next(ctx, r)
					},
					order: 20,
				},
				declared{MiddlewareFunc: adding(slog.String("added", "at 10")), order: 10},
			},
		})

		logger.InfoContext(t.Context(), "ordered")

		assert.Equal(t, []string{"added=at 10"}, seen)
	})

	t.Run("a lower order drops", func(t *testing.T) {
		var n int

		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.Middleware{
				declared{MiddlewareFunc: counting(&n), order: 20},
				declared{MiddlewareFunc: dropping, order: 10},
			},
		})

		logger.InfoContext(t.Context(), "dropped")

		assert.Zero(t, n)
		assert.Empty(t, rec.all())
	})

	t.Run("an undeclared order counts as zero", func(t *testing.T) {
		var seen []string

		logger, _ := newRecorded(t, log.Params{
			Middleware: []log.Middleware{
				declared{MiddlewareFunc: tracing(&seen, "five"), order: 5},
				tracing(&seen, "undeclared"),
			},
		})

		logger.InfoContext(t.Context(), "ordered")

		assert.Equal(t, []string{"undeclared", "five"}, seen)
	})

	t.Run("an order set with Ordered replaces a declared one", func(t *testing.T) {
		var seen []string

		logger, _ := newRecorded(t, log.Params{
			Middleware: []log.Middleware{
				declared{MiddlewareFunc: tracing(&seen, "fifty"), order: 50},
				log.Ordered(1, declared{MiddlewareFunc: tracing(&seen, "wrapped"), order: 100}),
			},
		})

		logger.InfoContext(t.Context(), "ordered")

		assert.Equal(t, []string{"wrapped", "fifty"}, seen)
	})

	t.Run("equal orders both run", func(t *testing.T) {
		var first, second int

		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.Middleware{counting(&first), counting(&second)},
		})

		logger.InfoContext(t.Context(), "both")

		assert.Equal(t, 1, first)
		assert.Equal(t, 1, second)
		assert.Len(t, rec.all(), 1)
	})
}

func TestOrderedOfAnOrdered(t *testing.T) {
	m := log.Ordered(1, log.Ordered(2, noop{}))

	assert.Equal(t, 1, m.Order())
	assert.Equal(t, log.Ordered(1, noop{}), m, "the inner wrapper is replaced, not wrapped")
}

func TestNilMiddlewareIsLeftOut(t *testing.T) {
	var seen []string

	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.Middleware{
			nil,
			declared{MiddlewareFunc: tracing(&seen, "twenty"), order: 20},
			(*pointer)(nil),
			log.MiddlewareFunc(nil),
			log.Ordered(5, nil),
			log.Ordered(5, log.Ordered(6, (*pointer)(nil))),
			declared{MiddlewareFunc: tracing(&seen, "ten"), order: 10},
		},
	})

	logger.InfoContext(t.Context(), "outside fx")

	assert.Equal(t, []string{"ten", "twenty"}, seen)
	assert.Len(t, rec.all(), 1)
}

func TestMiddlewareConstructorsReturningNilAreLeftOut(t *testing.T) {
	logger, rec := newModuleLogger(t,
		log.ProvideMiddleware(func() log.Middleware {
			return nil
		}),
		log.ProvideMiddleware(func() *pointer {
			return nil
		}),
		log.ProvideMiddleware(func() log.Middleware {
			return log.Ordered(5, (*pointer)(nil))
		}),
		log.ProvideMiddleware(func() log.Middleware {
			return adding(slog.String("feature", "on"))
		}),
	)

	logger.InfoContext(t.Context(), "constructed")

	assert.Equal(t, []string{"[feature=on]"}, rec.attrs())
}

func TestWithMiddlewareNil(t *testing.T) {
	logger, rec := newModuleLogger(t,
		log.WithMiddleware(nil),
		log.WithMiddleware((*pointer)(nil)),
	)

	logger.InfoContext(t.Context(), "supplied")

	assert.Len(t, rec.all(), 1)
}

// order is a value the graph supplies to a middleware constructor.
type order int

func TestMiddlewareValuesAndConstructorsAreOrderedTogether(t *testing.T) {
	logger, rec := newModuleLogger(t,
		log.WithMiddleware(declared{MiddlewareFunc: adding(slog.Int("order", 20)), order: 20}),
		fx.Supply(order(15)),
		log.ProvideMiddleware(func(o order) declared {
			return declared{MiddlewareFunc: adding(slog.Int("order", int(o))), order: int(o)}
		}),
		log.WithMiddleware(log.Ordered(10, adding(slog.Int("order", 10)))),
	)

	logger.InfoContext(t.Context(), "ordered")

	assert.Equal(t, []string{"[order=10 order=15 order=20]"}, rec.attrs())
}

func TestProvideMiddlewareTwiceInOneApp(t *testing.T) {
	logger, rec := newModuleLogger(t,
		log.ProvideMiddleware(func() log.Middleware {
			return log.Ordered(20, adding(slog.String("step", "second")))
		}),
		fx.Module("nested", log.ProvideMiddleware(func() log.Middleware {
			return log.Ordered(10, adding(slog.String("step", "first")))
		})),
	)

	logger.InfoContext(t.Context(), "twice")

	assert.Equal(t, []string{"[step=first step=second]"}, rec.attrs())
}
