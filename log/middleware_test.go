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

func TestMiddlewareSeesTheContext(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: addRequestID}},
	})

	logger.InfoContext(withRequestID(t, "r1"), "served")

	assert.Equal(t, []string{"[request_id=r1]"}, rec.attrs())
}

func TestMiddlewareDropsARecord(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: dropping}},
	})

	logger.InfoContext(t.Context(), "dropped")

	assert.Empty(t, rec.all())
}

func TestMiddlewareOrder(t *testing.T) {
	t.Run("a higher order sees a lower one's attribute", func(t *testing.T) {
		var seen []string

		logger, _ := newRecorded(t, log.Params{
			Middleware: []log.OrderedMiddleware{
				{
					Order: 20,
					Middleware: func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
						r.Attrs(func(a slog.Attr) bool {
							seen = append(seen, a.String())

							return true
						})

						return next(ctx, r)
					},
				},
				{Order: 10, Middleware: adding(slog.String("added", "at 10"))},
			},
		})

		logger.InfoContext(t.Context(), "ordered")

		assert.Equal(t, []string{"added=at 10"}, seen)
	})

	t.Run("a lower order drops", func(t *testing.T) {
		var n int

		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.OrderedMiddleware{
				{Order: 20, Middleware: counting(&n)},
				{Order: 10, Middleware: dropping},
			},
		})

		logger.InfoContext(t.Context(), "dropped")

		assert.Zero(t, n)
		assert.Empty(t, rec.all())
	})
}

// order is a value the graph supplies to a middleware constructor.
type order int

func TestMiddlewareValuesAndConstructorsAreOrderedTogether(t *testing.T) {
	var logger *slog.Logger

	rec := &recording{t: t}

	app := fxtest.New(t,
		log.Module,
		log.WithFormat(log.FormatText),
		log.WithTextHandler(rec.provide),
		log.WithMiddleware(20, adding(slog.Int("order", 20))),
		fx.Supply(order(15)),
		log.ProvideMiddleware(15, func(o order) log.Middleware {
			return adding(slog.Int("order", int(o)))
		}),
		log.WithMiddleware(10, adding(slog.Int("order", 10))),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "ordered")

	app.RequireStop()

	assert.Equal(t, []string{"[order=10 order=15 order=20]"}, rec.attrs())
}

func TestProvideMiddlewareTwiceInOneApp(t *testing.T) {
	var logger *slog.Logger

	rec := &recording{t: t}

	app := fxtest.New(t,
		log.Module,
		log.WithFormat(log.FormatText),
		log.WithTextHandler(rec.provide),
		log.ProvideMiddleware(20, func() log.Middleware {
			return adding(slog.String("step", "second"))
		}),
		fx.Module("nested", log.ProvideMiddleware(10, func() log.Middleware {
			return adding(slog.String("step", "first"))
		})),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "twice")

	app.RequireStop()

	assert.Equal(t, []string{"[step=first step=second]"}, rec.attrs())
}
