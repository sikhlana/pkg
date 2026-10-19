package trace_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"
)

// otelVars are every OpenTelemetry variable the module or the SDK reads that a test could trip over.
var otelVars = []string{
	"OTEL_SDK_DISABLED",
	"OTEL_SERVICE_NAME",
	"OTEL_RESOURCE_ATTRIBUTES",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
	"OTEL_EXPORTER_OTLP_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_CLIENT_KEY",
	"OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TIMEOUT",
	"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT",
	"OTEL_TRACES_EXPORTER",
	"OTEL_TRACES_SAMPLER",
	"OTEL_TRACES_SAMPLER_ARG",
}

// setup empties every variable in otelVars and restores OpenTelemetry's globals after the test, so neither the
// developer's shell nor another test can change its outcome.
func setup(t *testing.T) {
	t.Helper()

	for _, name := range otelVars {
		t.Setenv(name, "")
	}

	// A discarding sink, installed first so it's uninstalled last, keeps OpenTelemetry's complaints about the restore
	// below out of standard error.
	t.Cleanup(telemetry.InstallErrorSink(slog.New(slog.DiscardHandler)))

	handler := otel.GetErrorHandler()
	tp := otel.GetTracerProvider()            //nolint:forbidigo // the test restores the global it may change
	propagator := otel.GetTextMapPropagator() //nolint:forbidigo // the test restores the global it may change

	t.Cleanup(func() {
		otel.SetErrorHandler(handler)
		otel.SetTracerProvider(tp)            //nolint:forbidigo // restores the global the test may change
		otel.SetTextMapPropagator(propagator) //nolint:forbidigo // restores the global the test may change
	})
}

// request is an export request a collector received.
type request struct {
	Path        string
	ContentType string
	Header      http.Header
	Spans       []string
}

// collector is an OTLP HTTP trace collector on 127.0.0.1 that keeps every export request it receives.
type collector struct {
	*httptest.Server

	mu       sync.Mutex
	requests []request
}

// newCollector starts a plaintext collector, closed when the test ends.
func newCollector(t *testing.T) *collector {
	t.Helper()

	c := &collector{}
	c.Server = httptest.NewServer(http.HandlerFunc(c.handle(t)))

	t.Cleanup(c.Close)

	return c
}

// newTLSCollector starts a collector serving cert over TLS, closed when the test ends.
func newTLSCollector(t *testing.T, cert tls.Certificate) *collector {
	t.Helper()

	c := &collector{}
	c.Server = httptest.NewUnstartedServer(http.HandlerFunc(c.handle(t)))
	c.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	c.StartTLS()

	t.Cleanup(c.Close)

	return c
}

func (c *collector) handle(t *testing.T) func(http.ResponseWriter, *http.Request) {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		var export coltracepb.ExportTraceServiceRequest

		if r.Header.Get("Content-Type") == "application/json" {
			err = protojson.Unmarshal(body, &export)
		} else {
			err = proto.Unmarshal(body, &export)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		c.mu.Lock()
		c.requests = append(c.requests, request{
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Header:      r.Header.Clone(),
			Spans:       spanNames(&export),
		})
		c.mu.Unlock()

		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}
}

// all returns the requests received so far.
func (c *collector) all() []request {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.requests)
}

// spans returns the names of every span received so far.
func (c *collector) spans() []string {
	var names []string

	for _, r := range c.all() {
		names = append(names, r.Spans...)
	}

	return names
}

// spanNames returns the names of the spans in an export request.
func spanNames(export *coltracepb.ExportTraceServiceRequest) []string {
	var names []string

	for _, rs := range export.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				names = append(names, s.GetName())
			}
		}
	}

	return names
}

// run starts an app with the telemetry and trace modules, a logger over rec and opts, ends a span named served, then
// stops the app, which flushes the span.
func run(t *testing.T, rec *recorder, opts ...fx.Option) {
	t.Helper()

	var tp oteltrace.TracerProvider

	app := fxtest.New(t, append([]fx.Option{
		telemetry.Module,
		trace.Module,
		fx.Supply(slog.New(rec)),
		fx.Populate(&tp),
	}, opts...)...)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()
}

// startErr builds an app with the telemetry and trace modules and opts, and returns its construction error.
func startErr(opts ...fx.Option) error {
	app := fx.New(append([]fx.Option{
		fx.NopLogger,
		telemetry.Module,
		trace.Module,
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Invoke(func(oteltrace.TracerProvider) {}),
	}, opts...)...)

	return app.Err()
}

// closedPort returns the address of a port on 127.0.0.1 that nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := l.Addr().String()
	require.NoError(t, l.Close())

	return addr
}

// pki is a private CA and a leaf it issued for 127.0.0.1, with the CA's certificate written to CAFile.
type pki struct {
	CAFile string
	Leaf   tls.Certificate
}

// newPKI generates a CA and a leaf for 127.0.0.1, writing the CA's certificate into the test's temp dir.
func newPKI(t *testing.T) pki {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	leafDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "collector"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))

	return pki{
		CAFile: caFile,
		Leaf:   tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey},
	}
}

// recorder is a slog.Handler that keeps every record it's handed, at every level.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*recorder) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = append(r.records, rec.Clone())

	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler {
	return r
}

func (r *recorder) WithGroup(string) slog.Handler {
	return r
}

// warnings returns the Warn records handed to the recorder so far.
func (r *recorder) warnings() []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []slog.Record

	for _, rec := range r.records {
		if rec.Level == slog.LevelWarn {
			out = append(out, rec)
		}
	}

	return out
}

// hasError reports whether a record carries an error attribute.
func hasError(r slog.Record) bool {
	var found bool

	r.Attrs(func(a slog.Attr) bool {
		_, found = a.Value.Any().(error)

		return !found
	})

	return found
}

// attrValue returns the string value of a record's attribute, or empty when it has none.
func attrValue(r slog.Record, key string) string {
	var v string

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value.String()

			return false
		}

		return true
	})

	return v
}
