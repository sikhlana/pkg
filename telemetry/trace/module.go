package trace

import (
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

// Module provides the app's oteltrace.TracerProvider, built by New. It needs telemetry.Module and a *slog.Logger in
// the app.
var Module = fx.Module("telemetry/trace", fx.Provide(New))

// AsExporter annotates v, an Exporter or a constructor returning one, as the exporter New builds spans' exporter
// with. Wrap it in fx.Supply or fx.Provide.
func AsExporter(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"telemetry/trace:exporter::wired"`))
}

// WithExporter supplies the exporter New builds spans' exporter with, in place of OTLP over HTTP.
// otlpgrpc.ProvideExporter calls it.
func WithExporter(exporter Exporter) fx.Option {
	return fx.Supply(AsExporter(exporter))
}

// AsTracerProvider annotates v, a tracer provider or a constructor returning one of any concrete type, as the provider
// New returns unchanged in place of building one. The app owns its lifecycle. Wrap it in fx.Supply or fx.Provide.
func AsTracerProvider(v any) any {
	return fx.Annotate(
		v,
		fx.As(new(oteltrace.TracerProvider)),
		fx.ResultTags(`name:"telemetry/trace:provider::supplied"`),
	)
}

// WithTracerProvider supplies tp as the provider New returns unchanged in place of building one. The app owns its
// lifecycle.
func WithTracerProvider(tp oteltrace.TracerProvider) fx.Option {
	return fx.Supply(AsTracerProvider(tp))
}
