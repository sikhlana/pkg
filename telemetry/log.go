package telemetry

import (
	"context"
	"log/slog"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// LogMiddlewareOrder is the suggested order for registering LogMiddleware with log.WithMiddleware.
const LogMiddlewareOrder = 100

// LogMiddleware adds trace_id and span_id, as lowercase hex strings, to a record logged with a context carrying a
// valid span context, and passes every other record on unchanged. It has log.Middleware's signature, so register it
// with log.WithMiddleware. It reads the span context through the API, so it works with a supplied provider and when
// nothing is exported.
func LogMiddleware(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	sc := oteltrace.SpanContextFromContext(ctx)

	if sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}

	return next(ctx, r)
}
