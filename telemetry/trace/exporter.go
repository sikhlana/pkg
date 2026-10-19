package trace

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"

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
