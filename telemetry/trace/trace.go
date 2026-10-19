package trace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/telemetry"
)

// Params holds New's inputs. With fx, Logger comes from the app, Resource from telemetry.Module, and the rest are
// optional.
type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *slog.Logger
	Resource  *resource.Resource

	// ErrorSink orders New's stop hook before the sink is uninstalled, so errors from the final flush are logged.
	ErrorSink telemetry.ErrorSink

	// Telemetry is the operator's shared export settings.
	Telemetry telemetry.Config `optional:"true"`

	// Config is the operator's trace settings.
	Config Config `optional:"true"`

	// Exporter builds the span exporter. Without one, spans go over OTLP HTTP.
	Exporter Exporter `name:"telemetry/trace:exporter::wired" optional:"true"`

	// Supplied replaces the provider New would build. New returns it unchanged and never shuts it down.
	Supplied oteltrace.TracerProvider `name:"telemetry/trace:provider::supplied" optional:"true"`
}

// New returns the app's tracer provider. With Supplied, it returns that, logging a warning when the endpoint or the
// sampler is configured for traces too. Otherwise, when Telemetry doesn't enable traces, it returns a noop provider that carries an
// incoming span context forward and exports nothing. Otherwise it returns an SDK provider with Resource and the
// sampler Config names, and prepares it in the lifecycle: on start it builds the span exporter and attaches it
// through a batch span processor, and on stop it shuts the provider down, flushing buffered spans within the stop
// hook's deadline. Spans ended before start are dropped. Nothing that runs in the background exists until start, so
// an app that fails to start leaks nothing. A failed export or shutdown is logged at Warn, never returned: an
// unreachable collector neither fails startup nor stopping.
//
// New fails with telemetry.Config.Enabled's errors for a bad OTEL_TRACES_EXPORTER or endpoint variable, with
// ErrUnsupportedProtocol when the environment's protocol isn't one Exporter speaks, and with Config's validation
// errors for a sampler it can't build. Building the exporter, which reads the TLS files, fails start.
func New(p Params) (oteltrace.TracerProvider, error) {
	if p.Supplied != nil {
		warnUnused(p.Logger, p.Telemetry, p.Config)

		return p.Supplied, nil
	}

	enabled, enabledErr := p.Telemetry.Enabled("TRACES")
	if enabledErr != nil {
		return nil, enabledErr
	}

	if !enabled {
		return noop.NewTracerProvider(), nil
	}

	exporter := p.Exporter

	if exporter.Provider == nil {
		exporter = httpExporter
	}

	if err := checkProtocol(exporter.Protocols); err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(p.Resource)}

	// Passing a sampler stops the SDK reading OTEL_TRACES_SAMPLER and its argument, so one is passed only when the
	// Config names one.
	if p.Config.Sampler != "" {
		sampler, err := p.Config.sampler()
		if err != nil {
			return nil, fmt.Errorf("building sampler: %w", err)
		}

		opts = append(opts, sdktrace.WithSampler(sampler))
	}

	tp := sdktrace.NewTracerProvider(opts...)

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			spans, err := exporter.Provider(ctx, p.Telemetry, p.Logger)
			if err != nil {
				return fmt.Errorf("building span exporter: %w", err)
			}

			tp.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(spans))

			return nil
		},
		OnStop: func(ctx context.Context) error {
			if err := tp.Shutdown(ctx); err != nil {
				p.Logger.LogAttrs(
					ctx,
					slog.LevelWarn,
					"shutting down the tracer provider failed",
					slog.Any("error", err),
				)
			}

			return nil
		},
	})

	return tp, nil
}

// warnUnused logs a warning naming the settings a supplied provider leaves unused: the endpoint when the shared Config
// or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT sets one, and the sampler when Config or OTEL_TRACES_SAMPLER names one. The
// generic OTEL_EXPORTER_OTLP_ENDPOINT doesn't count, since other signals and components share it.
func warnUnused(logger *slog.Logger, shared telemetry.Config, cfg Config) {
	var unused []string

	if shared.Endpoint != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		unused = append(unused, "endpoint")
	}

	if cfg.Sampler != "" || os.Getenv("OTEL_TRACES_SAMPLER") != "" {
		unused = append(unused, "sampler")
	}

	if len(unused) > 0 {
		logger.Warn("telemetry settings are unused, since the app supplied a tracer provider",
			slog.String("unused", strings.Join(unused, ", ")))
	}
}
