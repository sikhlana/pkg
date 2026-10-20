package telemetry_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/sikhlana/pkg/telemetry"
)

func TestLogMiddleware(t *testing.T) {
	t.Parallel()

	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: oteltrace.TraceID{0xab, 1},
		SpanID:  oteltrace.SpanID{0xcd, 2},
	})

	tests := []struct {
		name string
		ctx  context.Context //nolint:containedctx // each case logs with its own context
		want map[string]string
	}{
		{
			name: "inside a span",
			ctx:  oteltrace.ContextWithSpanContext(t.Context(), sc),
			want: map[string]string{
				"trace_id": "ab010000000000000000000000000000",
				"span_id":  "cd02000000000000",
			},
		},
		{name: "without a span", ctx: t.Context()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := slog.NewRecord(time.Now(), slog.LevelInfo, "served", 0)
			r.AddAttrs(slog.String("route", "/users"))

			var got slog.Record

			err := telemetry.LogMiddleware{}.Handle(tt.ctx, r, func(_ context.Context, r slog.Record) error {
				got = r

				return nil
			})
			require.NoError(t, err)

			want := map[string]slog.Value{"route": slog.StringValue("/users")}

			for k, v := range tt.want {
				want[k] = slog.StringValue(v)
			}

			assert.Equal(t, want, attrs(got))
		})
	}
}

func TestLogMiddlewareOrder(t *testing.T) {
	t.Parallel()

	// A local interface, not log.OrderedMiddleware: importing log here would make telemetry release after it.
	var m interface{ Order() int } = telemetry.LogMiddleware{}

	assert.Equal(t, 500, m.Order())
}
