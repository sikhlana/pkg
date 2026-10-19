package otlpgrpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"
	"github.com/sikhlana/pkg/telemetry/trace/otlpgrpc"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// collector is an in-process OTLP gRPC trace service that keeps the spans and metadata it receives.
type collector struct {
	coltracepb.UnimplementedTraceServiceServer

	mu            sync.Mutex
	spans         []string
	authorization []string
}

func (c *collector) Export(
	ctx context.Context, req *coltracepb.ExportTraceServiceRequest,
) (*coltracepb.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.authorization = append(c.authorization, md.Get("authorization")...)

	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				c.spans = append(c.spans, s.GetName())
			}
		}
	}

	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// received returns the span names and authorization values received so far.
func (c *collector) received() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.spans), slices.Clone(c.authorization)
}

// newCollector serves a collector on 127.0.0.1:0 until the test ends, and returns it with its http:// endpoint.
func newCollector(t *testing.T) (*collector, string) {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	c := &collector{}
	srv := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(srv, c)

	go func() { _ = srv.Serve(l) }()

	t.Cleanup(srv.Stop)

	return c, "http://" + l.Addr().String()
}

// setup empties the variables the test depends on and restores OpenTelemetry's error handler after it.
func setup(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"OTEL_SDK_DISABLED", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS", "OTEL_EXPORTER_OTLP_INSECURE",
		"OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_TRACES_EXPORTER", "OTEL_TRACES_SAMPLER",
	} {
		t.Setenv(name, "")
	}

	// A discarding sink, installed first so it's uninstalled last, keeps OpenTelemetry's complaints about the restore
	// below out of standard error.
	t.Cleanup(telemetry.InstallErrorSink(slog.New(slog.DiscardHandler)))

	handler := otel.GetErrorHandler()

	t.Cleanup(func() { otel.SetErrorHandler(handler) })
}

func TestExportsOverGRPC(t *testing.T) {
	setup(t)

	c, endpoint := newCollector(t)

	var tp oteltrace.TracerProvider

	app := fxtest.New(t,
		telemetry.Module,
		trace.Module,
		otlpgrpc.ProvideExporter(),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(telemetry.Config{Endpoint: endpoint, Headers: map[string]string{"authorization": "Bearer x"}}),
		fx.Populate(&tp),
	)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()

	spans, authorization := c.received()
	assert.Equal(t, []string{"served"}, spans)
	assert.Equal(t, []string{"Bearer x"}, authorization)
}

func TestProtocolMustBeGRPC(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")

	_, endpoint := newCollector(t)

	app := fx.New(
		fx.NopLogger,
		telemetry.Module,
		trace.Module,
		otlpgrpc.ProvideExporter(),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(telemetry.Config{Endpoint: endpoint}),
		fx.Invoke(func(oteltrace.TracerProvider) {}),
	)

	require.ErrorIs(t, app.Err(), trace.ErrUnsupportedProtocol)
}

func TestGRPCProtocolAccepted(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	c, endpoint := newCollector(t)

	var tp oteltrace.TracerProvider

	app := fxtest.New(t,
		telemetry.Module,
		trace.Module,
		otlpgrpc.ProvideExporter(),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(telemetry.Config{Endpoint: endpoint}),
		fx.Populate(&tp),
	)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()

	spans, _ := c.received()
	assert.Equal(t, []string{"served"}, spans)
}

func TestTLSOnlyForHTTPS(t *testing.T) {
	setup(t)

	c, endpoint := newCollector(t)

	var tp oteltrace.TracerProvider

	// An unvalidated Config pairing TLS with a plaintext endpoint still dials in plaintext, as the HTTP exporter does.
	app := fxtest.New(t,
		telemetry.Module,
		trace.Module,
		otlpgrpc.ProvideExporter(),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(telemetry.Config{
			Endpoint: endpoint,
			TLS:      tlsconfig.ClientConfig{Enabled: true, CAFile: filepath.Join(t.TempDir(), "ca.pem")},
		}),
		fx.Populate(&tp),
	)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()

	spans, _ := c.received()
	assert.Equal(t, []string{"served"}, spans)
}

func TestPlaintextConfigBeatsEnvironmentTLS(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", writeCA(t))

	c, endpoint := newCollector(t)

	var tp oteltrace.TracerProvider

	app := fxtest.New(t,
		telemetry.Module,
		trace.Module,
		otlpgrpc.ProvideExporter(),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Supply(telemetry.Config{Endpoint: endpoint}),
		fx.Populate(&tp),
	)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()

	spans, _ := c.received()
	assert.Equal(t, []string{"served"}, spans, "the http endpoint dials in plaintext despite the environment's CA")
}

// writeCA writes a self-signed CA certificate into the test's temp dir and returns its path.
func writeCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))

	return path
}
