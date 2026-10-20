package telemetry

import (
	"context"
	"log/slog"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// LogMiddleware adds trace_id and span_id, as lowercase hex strings, to a record logged with a context carrying a
// valid span context, and passes every other record on unchanged. It implements log.Middleware, whose Handle takes
// (ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) and returns an error, so register
// its zero value with log.WithMiddleware. It reads the span context through the API, so it works with a supplied
// provider and when nothing is exported.
type LogMiddleware struct{}

// Handle adds the span context's ids to r, when there is a valid one, and passes it to next.
func (LogMiddleware) Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	sc := oteltrace.SpanContextFromContext(ctx)

	if sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}

	return next(ctx, r)
}

// Order returns 500, log's band for middleware that adds attributes, so samplers ordered below it drop records before
// it runs and redactors ordered above it see the ids.
func (LogMiddleware) Order() int {
	return 500
}
