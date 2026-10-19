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

	"github.com/sikhlana/pkg/telemetry"
)

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
