package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

type requestIDKey struct{}

// addRequestID is a middleware that adds the context's request id at the top level.
func addRequestID(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}

	return next(ctx, r)
}

// adding returns a middleware that adds attrs to every record.
func adding(attrs ...slog.Attr) log.Middleware {
	return func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
		r.AddAttrs(attrs...)

		return next(ctx, r)
	}
}

// counting returns a middleware that counts the records it sees and passes them on.
func counting(n *int) log.Middleware {
	return func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
		*n++

		return next(ctx, r)
	}
}

// dropping is a middleware that drops every record.
func dropping(context.Context, slog.Record, func(context.Context, slog.Record) error) error {
	return nil
}

// redacting is a middleware that replaces the value of every top-level token attribute.
func redacting(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "token" {
			a = slog.String("token", "REDACTED")
		}

		out.AddAttrs(a)

		return true
	})

	return next(ctx, out)
}

func withRequestID(t *testing.T, id string) context.Context {
	t.Helper()

	return context.WithValue(t.Context(), requestIDKey{}, id)
}

func TestHandlerConformance(t *testing.T) {
	var buf bytes.Buffer

	logger, err := log.New(log.Params{
		Destination: &buf,
		Middleware: []log.OrderedMiddleware{
			{Order: 10, Middleware: adding(slog.Bool("middleware", true))},
			{Order: 20, Middleware: addRequestID},
		},
	})
	require.NoError(t, err)

	slogtest.Run(t,
		func(*testing.T) slog.Handler {
			buf.Reset()

			return logger.Handler()
		},
		func(t *testing.T) map[string]any {
			lines := decodeLines(t, buf.Bytes())
			require.Len(t, lines, 1)

			return lines[0]
		},
	)
}

func TestMiddlewareSeesTheContext(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: addRequestID}},
	})

	logger.InfoContext(withRequestID(t, "r1"), "served")

	assert.Equal(t, []string{"[request_id=r1]"}, rec.attrs())
}

func TestMiddlewareRunsOnADerivedLogger(t *testing.T) {
	var n int

	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: counting(&n)}},
	})

	logger.With(slog.String("server", "api")).WithGroup("http").InfoContext(t.Context(), "served")

	assert.Equal(t, 1, n)
	assert.Len(t, rec.all(), 1)
}

func TestMiddlewareRunsOnARecordHandedToTheHandler(t *testing.T) {
	var n int

	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: counting(&n)}},
	})

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "handed", 0)
	require.NoError(t, logger.Handler().Handle(t.Context(), r))

	assert.Equal(t, 1, n)
	assert.Len(t, rec.all(), 1)
}

func TestMiddlewareDropsARecord(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: dropping}},
	})

	logger.InfoContext(t.Context(), "dropped")

	assert.Empty(t, rec.all())
}

func TestMiddlewareNeverSeesARecordBelowTheLevel(t *testing.T) {
	var n int

	logger, _ := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: counting(&n)}},
	})

	logger.DebugContext(t.Context(), "hidden")

	assert.Zero(t, n)
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

func TestMiddlewareAttributesLandAtTheTopLevel(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Middleware: addRequestID}},
	})

	logger.WithGroup("http").InfoContext(withRequestID(t, "r1"), "served", slog.Int("status", 200))

	assert.Equal(t, []string{"[http=[status=200] request_id=r1]"}, rec.attrs())
}

func TestAttributesBeforeAndAfterAGroup(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})

	logger.With(slog.String("server", "api")).
		WithGroup("http").
		With(slog.String("route", "/users")).
		InfoContext(t.Context(), "served", slog.Int("status", 200))

	assert.Equal(t, []string{"[server=api http=[route=/users status=200]]"}, rec.attrs())
}

func TestMiddlewareSeesAttributesAttachedWithWith(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.OrderedMiddleware{{Order: 900, Middleware: redacting}},
	})

	logger.With(slog.String("token", "secret1")).InfoContext(t.Context(), "attached")
	logger.InfoContext(t.Context(), "passed", slog.String("token", "secret2"))

	assert.Equal(t, []string{"[token=REDACTED]", "[token=REDACTED]"}, rec.attrs())
}

func TestAttributesKeepTheirOrder(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})

	logger.With(slog.String("a", "1"), slog.GroupAttrs("g", slog.String("x", "1")), slog.String("b", "2")).
		With(slog.String("server", "api")).
		InfoContext(t.Context(), "ordered", slog.String("c", "3"))

	assert.Equal(t, []string{"[a=1 g=[x=1] b=2 server=api c=3]"}, rec.attrs())
}

func TestWithAttrsCostsTheSameWhateverTheMiddleware(t *testing.T) {
	withAttrsAllocs := func(middleware []log.OrderedMiddleware) float64 {
		logger, err := log.New(log.Params{Destination: io.Discard, Middleware: middleware})
		require.NoError(t, err)

		h := logger.Handler()
		attrs := []slog.Attr{slog.String("request_id", "r1")}

		return testing.AllocsPerRun(100, func() {
			_ = h.WithAttrs(attrs)
		})
	}

	none := withAttrsAllocs(nil)
	five := withAttrsAllocs([]log.OrderedMiddleware{
		{Order: 1, Middleware: addRequestID},
		{Order: 2, Middleware: addRequestID},
		{Order: 3, Middleware: addRequestID},
		{Order: 4, Middleware: addRequestID},
		{Order: 5, Middleware: addRequestID},
	})

	assert.InDelta(t, none, five, 0, "allocations per WithAttrs without middleware and with five")
}

func TestGroupsWithTheSameKeyMerge(t *testing.T) {
	t.Run("a middleware's group and the caller's", func(t *testing.T) {
		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.OrderedMiddleware{
				{Middleware: adding(slog.GroupAttrs("http", slog.String("request_id", "r1")))},
			},
		})

		logger.InfoContext(t.Context(), "served", slog.GroupAttrs("http", slog.String("method", "GET")))

		assert.Equal(t, []string{"[http=[method=GET request_id=r1]]"}, rec.attrs())
	})

	t.Run("nested groups", func(t *testing.T) {
		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.OrderedMiddleware{
				{Middleware: adding(slog.GroupAttrs("network", slog.GroupAttrs("client", slog.String("geo", "GB"))))},
			},
		})

		logger.InfoContext(t.Context(), "served",
			slog.GroupAttrs("network", slog.GroupAttrs("client", slog.String("ip", "10.0.0.1"))),
		)

		assert.Equal(t, []string{"[network=[client=[ip=10.0.0.1 geo=GB]]]"}, rec.attrs())
	})

	t.Run("a group attached to a derived logger", func(t *testing.T) {
		logger, rec := newRecorded(t, log.Params{})

		logger.With(slog.GroupAttrs("http", slog.String("route", "/users"))).
			InfoContext(t.Context(), "served", slog.GroupAttrs("http", slog.Int("status_code", 200)))

		assert.Equal(t, []string{"[http=[route=/users status_code=200]]"}, rec.attrs())
	})

	t.Run("inside an open group", func(t *testing.T) {
		var buf bytes.Buffer

		logger, err := log.New(log.Params{Destination: &buf})
		require.NoError(t, err)

		logger.WithGroup("req").
			With(slog.GroupAttrs("http", slog.String("route", "/users"))).
			InfoContext(t.Context(), "served", slog.GroupAttrs("http", slog.Int("status_code", 200)))

		assert.Equal(t, 1, strings.Count(buf.String(), `"http":`))

		var line struct {
			Req struct {
				HTTP map[string]any `json:"http"`
			} `json:"req"`
		}

		require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
		assert.Equal(t, map[string]any{"route": "/users", "status_code": float64(200)}, line.Req.HTTP)
	})

	t.Run("plain duplicates", func(t *testing.T) {
		logger, rec := newRecorded(t, log.Params{
			Middleware: []log.OrderedMiddleware{{Middleware: adding(slog.String("request_id", "b"))}},
		})

		logger.InfoContext(t.Context(), "served", slog.String("request_id", "a"))

		assert.Equal(t, []string{"[request_id=a request_id=b]"}, rec.attrs())
	})

	t.Run("as one JSON key", func(t *testing.T) {
		var buf bytes.Buffer

		logger, err := log.New(log.Params{
			Destination: &buf,
			Middleware: []log.OrderedMiddleware{
				{Middleware: adding(slog.GroupAttrs("http", slog.String("request_id", "r1")))},
			},
		})
		require.NoError(t, err)

		logger.InfoContext(t.Context(), "served", slog.GroupAttrs("http", slog.String("method", "GET")))

		assert.Equal(t, 1, strings.Count(buf.String(), `"http":`))

		var line struct {
			HTTP map[string]string `json:"http"`
		}

		require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
		assert.Equal(t, map[string]string{"method": "GET", "request_id": "r1"}, line.HTTP)
	})
}

func TestMergingDoesNotAllocateWithoutDuplicates(t *testing.T) {
	logger, err := log.New(log.Params{
		Format: log.FormatText,
		TextHandler: func(_ io.Writer, _ *slog.HandlerOptions) slog.Handler {
			return slog.DiscardHandler
		},
	})
	require.NoError(t, err)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "distinct", 0)
	r.AddAttrs(
		slog.GroupAttrs("http", slog.Int("status_code", 200)),
		slog.GroupAttrs("network", slog.String("ip", "10.0.0.1")),
		slog.String("request_id", "r1"),
	)

	h := logger.Handler()
	ctx := t.Context()

	allocs := testing.AllocsPerRun(100, func() {
		_ = h.Handle(ctx, r)
	})

	assert.Zero(t, allocs, "%v allocations per record", allocs)
}
