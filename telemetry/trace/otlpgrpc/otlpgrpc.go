// Package otlpgrpc exports telemetry/trace's spans over OTLP gRPC instead of HTTP, so only the apps that import it
// build gRPC.
package otlpgrpc

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"
)

// New is the trace.ExporterProvider for OTLP over gRPC. An endpoint from the Config is dialled at its
// host and port, ignoring any path; its scheme picks plaintext (http) or TLS (https). It connects lazily, so an
// unreachable collector doesn't fail it.
func New(ctx context.Context, cfg telemetry.Config, logger *slog.Logger) (sdktrace.SpanExporter, error) {
	var opts []otlptracegrpc.Option

	if cfg.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}

	// Credentials win over the plaintext an http endpoint asks for, so they're passed only for an https one.
	switch {
	case cfg.UseTLS():
		tlsCfg, err := cfg.TLS.TLSConfig(logger)
		if err != nil {
			return nil, fmt.Errorf("building tls config: %w", err)
		}

		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)))
	case cfg.Plaintext():
		// TLS settings from the environment become credentials that beat the http scheme's plaintext, and only a dial
		// option, applied after the exporter's own, overrides them.
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building otlp grpc exporter: %w", err)
	}

	return exporter, nil
}

// ProvideExporter wires New as trace.Module's exporter, speaking trace.ProtocolGRPC.
func ProvideExporter() fx.Option {
	return trace.WithExporter(trace.Exporter{Provider: New, Protocols: []string{trace.ProtocolGRPC}})
}
