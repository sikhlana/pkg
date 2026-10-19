package trace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/telemetry"
)

// The OTLP protocols, as OTEL_EXPORTER_OTLP_PROTOCOL names them.
const (
	// ProtocolHTTPProtobuf is OTLP over HTTP with protobuf payloads, the HTTP exporter's default.
	ProtocolHTTPProtobuf = "http/protobuf"

	// ProtocolHTTPJSON is OTLP over HTTP with JSON payloads.
	ProtocolHTTPJSON = "http/json"

	// ProtocolGRPC is OTLP over gRPC.
	ProtocolGRPC = "grpc"
)

// ErrUnsupportedProtocol reports an OTEL_EXPORTER_OTLP_TRACES_PROTOCOL or OTEL_EXPORTER_OTLP_PROTOCOL naming a
// protocol the app's exporter doesn't speak.
var ErrUnsupportedProtocol = errors.New("unsupported otlp protocol")

// ExporterProvider builds the span exporter from the operator's shared Config. A field that's set is passed to the
// exporter as an option; an empty one is left for the exporter to read from the environment. TLS settings are used
// only when cfg.UseTLS reports so, and log through logger.
type ExporterProvider func(ctx context.Context, cfg telemetry.Config, logger *slog.Logger) (
	sdktrace.SpanExporter, error)

// Exporter pairs an ExporterProvider with the OTLP protocols its exporters speak, which the environment's protocol
// is checked against.
type Exporter struct {
	Provider  ExporterProvider
	Protocols []string
}

// httpExporter is the exporter New builds when the app wires none.
var httpExporter = Exporter{Provider: NewHTTPExporter, Protocols: []string{ProtocolHTTPProtobuf, ProtocolHTTPJSON}}

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
	Exporter Exporter `name:"telemetry/trace:exporter:wired" optional:"true"`

	// Supplied replaces the provider New would build. New returns it unchanged and never shuts it down.
	Supplied oteltrace.TracerProvider `name:"telemetry/trace:provider:supplied" optional:"true"`
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

// NewHTTPExporter is the ExporterProvider for OTLP over HTTP. An endpoint from the Config has /v1/traces appended, as
// OTEL_EXPORTER_OTLP_ENDPOINT does. The payload encoding is left to OTEL_EXPORTER_OTLP_TRACES_PROTOCOL and
// OTEL_EXPORTER_OTLP_PROTOCOL, protobuf when neither is set.
func NewHTTPExporter(ctx context.Context, cfg telemetry.Config, logger *slog.Logger) (sdktrace.SpanExporter, error) {
	var opts []otlptracehttp.Option

	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing endpoint: %w", err)
		}

		opts = append(opts, otlptracehttp.WithEndpointURL(u.JoinPath("v1/traces").String()))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}

	switch {
	case cfg.UseTLS():
		tlsCfg, err := cfg.TLS.TLSConfig(logger)
		if err != nil {
			return nil, fmt.Errorf("building tls config: %w", err)
		}

		opts = append(opts, otlptracehttp.WithTLSClientConfig(tlsCfg))
	case cfg.Plaintext():
		// The SDK would otherwise refuse to start with TLS settings from the environment beside an http endpoint.
		opts = append(opts, otlptracehttp.WithTLSClientConfig(nil))
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building otlp http exporter: %w", err)
	}

	return exporter, nil
}

// checkProtocol fails with ErrUnsupportedProtocol when OTEL_EXPORTER_OTLP_TRACES_PROTOCOL, or failing that
// OTEL_EXPORTER_OTLP_PROTOCOL, names a protocol outside protocols, compared trimmed and case-insensitively.
func checkProtocol(protocols []string) error {
	for _, name := range []string{"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))

		if v == "" {
			continue
		}

		if slices.Contains(protocols, v) {
			return nil
		}

		return fmt.Errorf("%w: %s is %q, and the exporter speaks %s",
			ErrUnsupportedProtocol, name, v, strings.Join(protocols, ", "))
	}

	return nil
}

// sampler returns the SDK sampler the Config names, nil when it names none, or Validate's error. New checks it again,
// so a Config that skipped validation fails startup rather than sampling nothing.
func (c Config) sampler() (sdktrace.Sampler, error) {
	switch c.Sampler {
	case "":
		return c.withoutArg(nil, ErrSamplerArgWithoutSampler)
	case SamplerAlwaysOn:
		return c.withoutArg(sdktrace.AlwaysSample(), ErrSamplerArgUnused)
	case SamplerAlwaysOff:
		return c.withoutArg(sdktrace.NeverSample(), ErrSamplerArgUnused)
	case SamplerParentBasedAlwaysOn:
		return c.withoutArg(sdktrace.ParentBased(sdktrace.AlwaysSample()), ErrSamplerArgUnused)
	case SamplerParentBasedAlwaysOff:
		return c.withoutArg(sdktrace.ParentBased(sdktrace.NeverSample()), ErrSamplerArgUnused)
	case SamplerTraceIDRatio, SamplerParentBasedTraceIDRatio:
		ratio, err := c.ratio()
		if err != nil {
			return nil, err
		}

		if c.Sampler == SamplerTraceIDRatio {
			return sdktrace.TraceIDRatioBased(ratio), nil
		}

		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	default:
		return nil, &configvalidator.Error{Key: "sampler", Err: ErrInvalidSampler}
	}
}

// withoutArg returns sampler, or err under sampler_arg's key when SamplerArg is set, since the sampler takes none.
func (c Config) withoutArg(sampler sdktrace.Sampler, err error) (sdktrace.Sampler, error) {
	if c.SamplerArg != "" {
		return nil, &configvalidator.Error{Key: "sampler_arg", Err: err}
	}

	return sampler, nil
}

// ratio returns SamplerArg as a ratio, 1 when it's empty, or ErrInvalidSamplerArg under sampler_arg's key.
func (c Config) ratio() (float64, error) {
	if c.SamplerArg == "" {
		return 1, nil
	}

	r, err := strconv.ParseFloat(c.SamplerArg, 64)

	if err != nil || math.IsNaN(r) || r < 0 || r > 1 {
		return 0, &configvalidator.Error{Key: "sampler_arg", Err: ErrInvalidSamplerArg}
	}

	return r, nil
}
