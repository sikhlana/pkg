package telemetry_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/sikhlana/pkg/telemetry"
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
	"OTEL_TRACES_EXPORTER",
	"OTEL_TRACES_SAMPLER",
	"OTEL_TRACES_SAMPLER_ARG",
}

// clearEnv empties every variable in otelVars for the test, so the developer's shell can't change its outcome.
func clearEnv(t *testing.T) {
	t.Helper()

	for _, name := range otelVars {
		t.Setenv(name, "")
	}
}

// restoreGlobals puts OpenTelemetry's global error handler, tracer provider and propagator back after the test.
func restoreGlobals(t *testing.T) {
	t.Helper()

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

// all returns the records handed to the recorder so far.
func (r *recorder) all() []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.records)
}

// attrs returns a record's attributes by key.
func attrs(r slog.Record) map[string]slog.Value {
	out := make(map[string]slog.Value)

	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value

		return true
	})

	return out
}

// resourceAttrs returns a resource's attributes as strings, by key.
func resourceAttrs(res *resource.Resource) map[attribute.Key]string {
	out := make(map[attribute.Key]string)

	for _, kv := range res.Attributes() {
		out[kv.Key] = kv.Value.String()
	}

	return out
}
