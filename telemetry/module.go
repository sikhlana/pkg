package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

// Module provides the *resource.Resource and the propagation.TextMapPropagator the signals share, and the
// ErrorSink, which sends OpenTelemetry's own errors and log messages to the app's *slog.Logger while the app runs. It
// doesn't touch OpenTelemetry's global tracer provider or propagator; list InstallGlobals for that. The signals read
// the operator's Config from the app themselves.
var Module = fx.Module("telemetry",
	fx.Provide(NewResource, NewPropagator, NewErrorSink),
	fx.Invoke(func(ErrorSink) {}),
)

// InstallGlobals sets OpenTelemetry's global propagator to the app's and, when the app has one, its global tracer
// provider, for third-party libraries that can only read the globals. It sets them when the app is built, so code
// that reads them in an invoke sees them, and never puts back what it replaced: OpenTelemetry's default global
// provider is a proxy that delegates to the first provider set, for good, so there's nothing to restore. The globals
// are process-wide; list InstallGlobals in an app, not in tests that build several apps in one process.
var InstallGlobals = fx.Invoke(func(p globalsParams) {
	otel.SetTextMapPropagator(p.Propagator) //nolint:forbidigo // the one place that sets the globals

	if p.TracerProvider != nil {
		otel.SetTracerProvider(p.TracerProvider) //nolint:forbidigo // the one place that sets the globals
	}
})

// globalsParams is InstallGlobals' input.
type globalsParams struct {
	fx.In

	Propagator     propagation.TextMapPropagator
	TracerProvider oteltrace.TracerProvider `optional:"true"`
}
