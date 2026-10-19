package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/slogtest"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/sikhlana/pkg/telemetry"
)

func TestResource(t *testing.T) {
	tests := []struct {
		name    string
		params  telemetry.ResourceParams
		env     map[string]string
		want    map[attribute.Key]string
		missing []attribute.Key
	}{
		{
			name:    "nothing wired",
			missing: []attribute.Key{"service.version"},
		},
		{
			name:   "wiring alone",
			params: telemetry.ResourceParams{ServiceName: "notary", ServiceVersion: "1.2.3"},
			want:   map[attribute.Key]string{"service.name": "notary", "service.version": "1.2.3"},
		},
		{
			name:   "the environment renames the service",
			params: telemetry.ResourceParams{ServiceName: "notary", ServiceVersion: "1.2.3"},
			env:    map[string]string{"OTEL_SERVICE_NAME": "notary-canary"},
			want:   map[attribute.Key]string{"service.name": "notary-canary", "service.version": "1.2.3"},
		},
		{
			name:   "resource attributes from the environment",
			params: telemetry.ResourceParams{ServiceName: "notary"},
			env:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment.name=prod,service.version=9"},
			want: map[attribute.Key]string{
				"service.name": "notary", "service.version": "9", "deployment.environment.name": "prod",
			},
		},
		{
			name:   "OTEL_SERVICE_NAME beats the resource attributes",
			params: telemetry.ResourceParams{ServiceName: "notary"},
			env: map[string]string{
				"OTEL_SERVICE_NAME":        "from-name",
				"OTEL_RESOURCE_ATTRIBUTES": "service.name=from-attributes",
			},
			want: map[attribute.Key]string{"service.name": "from-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			res, err := telemetry.NewResource(tt.params)
			require.NoError(t, err)

			got := resourceAttrs(res)

			for k, v := range tt.want {
				assert.Equal(t, v, got[k], k)
			}

			for _, k := range tt.missing {
				assert.NotContains(t, got, k)
			}

			assert.Equal(t, "opentelemetry", got["telemetry.sdk.name"])

			if tt.params.ServiceName == "" && tt.env["OTEL_SERVICE_NAME"] == "" {
				assert.Contains(t, got["service.name"], "unknown_service")
			}
		})
	}
}

func TestPropagatorRoundTrip(t *testing.T) {
	t.Parallel()

	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1, 2, 3},
		SpanID:     oteltrace.SpanID{4, 5, 6},
		TraceFlags: oteltrace.FlagsSampled,
	})

	member, err := baggage.NewMember("tenant", "acme")
	require.NoError(t, err)

	bag, err := baggage.New(member)
	require.NoError(t, err)

	ctx := baggage.ContextWithBaggage(oteltrace.ContextWithSpanContext(t.Context(), sc), bag)
	carrier := propagation.MapCarrier{}
	p := telemetry.NewPropagator()

	p.Inject(ctx, carrier)

	assert.Contains(t, carrier, "traceparent")
	assert.Contains(t, carrier, "baggage")

	got := p.Extract(t.Context(), carrier)

	assert.Equal(t, sc.TraceID(), oteltrace.SpanContextFromContext(got).TraceID())
	assert.Equal(t, sc.SpanID(), oteltrace.SpanContextFromContext(got).SpanID())
	assert.Equal(t, "acme", baggage.FromContext(got).Member("tenant").Value())
}

func TestResourceKeepsWhatParsed(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod,service.name")

	rec := &recorder{}

	res, err := telemetry.NewResource(telemetry.ResourceParams{Logger: slog.New(rec), ServiceName: "notary"})
	require.NoError(t, err)

	got := resourceAttrs(res)
	assert.Equal(t, "prod", got["deployment.environment.name"])
	assert.Equal(t, "notary", got["service.name"])

	records := rec.all()
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.ErrorIs(t, attrs(records[0])["error"].Any().(error), resource.ErrPartialResource)
}

func TestInstallErrorSink(t *testing.T) {
	restoreGlobals(t)

	rec := &recorder{}
	errRefused := errors.New("connection refused")

	uninstall := telemetry.InstallErrorSink(slog.New(rec))

	otel.Handle(errRefused)
	uninstall()
	otel.Handle(errors.New("after uninstall"))

	records := rec.all()
	require.Len(t, records, 1, "dropped once uninstalled")
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.Equal(t, errRefused, attrs(records[0])["error"].Any())
}

func TestInstallErrorSinkLatestWins(t *testing.T) {
	restoreGlobals(t)

	first, second := &recorder{}, &recorder{}

	uninstallFirst := telemetry.InstallErrorSink(slog.New(first))
	uninstallSecond := telemetry.InstallErrorSink(slog.New(second))

	otel.Handle(errors.New("to the second"))
	uninstallFirst()
	otel.Handle(errors.New("still to the second"))
	uninstallSecond()

	assert.Empty(t, first.all())
	assert.Len(t, second.all(), 2, "uninstalling a replaced logger leaves the newer one")
}

func TestForwarderFollowsTheHandlerRules(t *testing.T) {
	restoreGlobals(t)

	var buf bytes.Buffer

	uninstall := telemetry.InstallErrorSink(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer uninstall()

	err := slogtest.TestHandler(telemetry.Forwarder(), func() []map[string]any {
		var out []map[string]any

		for line := range bytes.Lines(buf.Bytes()) {
			var m map[string]any

			if jsonErr := json.Unmarshal(line, &m); jsonErr != nil {
				panic(jsonErr)
			}

			out = append(out, m)
		}

		return out
	})

	require.NoError(t, err)
}

func TestForwarderFallbackGetsRecordsWhenNothingIsInstalled(t *testing.T) {
	handler := otel.GetErrorHandler()

	t.Cleanup(func() { otel.SetErrorHandler(handler) })

	rec := &recorder{}
	t.Cleanup(telemetry.SetFallback(rec))

	uninstall := telemetry.InstallErrorSink(slog.New(slog.DiscardHandler))
	uninstall()

	otel.Handle(errors.New("nobody's listening"))

	records := rec.all()
	require.Len(t, records, 1, "the fallback gets it rather than nobody")
	assert.Equal(t, slog.LevelWarn, records[0].Level)
}

func TestInstallErrorSinkNilLoggerDiscards(t *testing.T) {
	restoreGlobals(t)

	uninstall := telemetry.InstallErrorSink(nil)
	defer uninstall()

	assert.NotPanics(t, func() { otel.Handle(errors.New("boom")) })
}

func TestInstallErrorSinkFallsBackToTheEarlierLogger(t *testing.T) {
	restoreGlobals(t)

	first, second := &recorder{}, &recorder{}

	uninstallFirst := telemetry.InstallErrorSink(slog.New(first))
	defer uninstallFirst()

	uninstallSecond := telemetry.InstallErrorSink(slog.New(second))

	otel.Handle(errors.New("to the second"))
	uninstallSecond()
	uninstallSecond()
	otel.Handle(errors.New("back to the first"))

	assert.Len(t, second.all(), 1)
	require.Len(t, first.all(), 1, "the first logger is still installed, and an extra uninstall doesn't remove it")
	assert.Equal(t, "back to the first", attrs(first.all()[0])["error"].Any().(error).Error())
}

func TestOpenTelemetryLevels(t *testing.T) {
	restoreGlobals(t)

	var buf bytes.Buffer

	uninstall := telemetry.InstallErrorSink(
		slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	)
	defer uninstall()

	// OpenTelemetry logs warnings at V(1), informational messages at V(4) and debug messages at V(8).
	logger := logr.FromSlogHandler(telemetry.LogrForwarder())
	logger.V(1).Info("a warning")
	logger.V(4).Info("for information")
	logger.V(8).Info("for debugging")
	logger.Error(errors.New("failed"), "an error")

	var levels []string

	for line := range bytes.Lines(buf.Bytes()) {
		var m map[string]any

		require.NoError(t, json.Unmarshal(line, &m))

		levels = append(levels, fmt.Sprint(m["level"], " ", m["msg"]))
	}

	assert.Equal(
		t,
		[]string{"WARN a warning", "ERROR an error"},
		levels,
		"informational and debug messages are below Info",
	)
}

func TestForwarderBuildsItsHandlerOncePerLogger(t *testing.T) {
	restoreGlobals(t)

	first, second := &countingHandler{}, &countingHandler{}

	uninstallFirst := telemetry.InstallErrorSink(slog.New(first))
	defer uninstallFirst()

	h := telemetry.Forwarder().WithAttrs([]slog.Attr{slog.String("scope", "sdk")})

	for range 3 {
		assert.True(t, h.Enabled(t.Context(), slog.LevelWarn))
		require.NoError(t, h.Handle(t.Context(), slog.Record{}))
	}

	uninstallSecond := telemetry.InstallErrorSink(slog.New(second))
	defer uninstallSecond()

	require.NoError(t, h.Handle(t.Context(), slog.Record{}))

	assert.EqualValues(t, 1, first.withAttrs.Load())
	assert.EqualValues(t, 3, first.handled.Load())
	assert.EqualValues(t, 1, second.withAttrs.Load(), "rebuilt for the new logger")
	assert.EqualValues(t, 1, second.handled.Load())
}

// countingHandler counts how often its handlers are derived with WithAttrs and how many records they handle.
type countingHandler struct {
	withAttrs atomic.Int64
	handled   atomic.Int64
}

func (*countingHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (c *countingHandler) Handle(context.Context, slog.Record) error {
	c.handled.Add(1)

	return nil
}

func (c *countingHandler) WithAttrs([]slog.Attr) slog.Handler {
	c.withAttrs.Add(1)

	return c
}

func (c *countingHandler) WithGroup(string) slog.Handler {
	return c
}

// resourceAttrs returns a resource's attributes as strings, by key.
func resourceAttrs(res *resource.Resource) map[attribute.Key]string {
	out := make(map[attribute.Key]string)

	for _, kv := range res.Attributes() {
		out[kv.Key] = kv.Value.String()
	}

	return out
}
