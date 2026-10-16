package tint_test

import (
	"bytes"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
	"github.com/sikhlana/pkg/log/tint"
)

const escape = "\x1b["

func terminal(int) bool { return true }

func TestNoColourToABuffer(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer

	slog.New(tint.New(&buf, nil)).ErrorContext(t.Context(), "plain", slog.Int("status", 500))

	assert.Contains(t, buf.String(), "plain")
	assert.NotContains(t, buf.String(), escape)
}

func TestColourOnlyOnATerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	assert.True(t, tint.Colour(os.Stderr, terminal), "a terminal")
	assert.False(t, tint.Colour(os.Stderr, func(int) bool { return false }), "a file that isn't a terminal")
	assert.False(t, tint.Colour(&bytes.Buffer{}, terminal), "a writer that isn't a file")
}

func TestNoColourWithNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	assert.False(t, tint.Colour(os.Stderr, terminal))

	var buf bytes.Buffer

	slog.New(tint.New(&buf, nil)).ErrorContext(t.Context(), "plain")

	assert.NotContains(t, buf.String(), escape)
}

func TestLevelAndSource(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(tint.New(&buf, &slog.HandlerOptions{Level: slog.LevelWarn, AddSource: true}))

	logger.InfoContext(t.Context(), "hidden")
	assert.Empty(t, buf.String())

	_, _, line, _ := runtime.Caller(0)
	logger.WarnContext(t.Context(), "located", slog.Int("want_line", line+1))

	assert.Contains(t, buf.String(), "tint_test.go:"+strconv.Itoa(line+1))
	assert.Contains(t, buf.String(), "located")
}

func TestProvideTextHandler(t *testing.T) {
	var (
		buf    bytes.Buffer
		logger *slog.Logger
	)

	app := fxtest.New(t,
		log.Module,
		log.WithFormat(log.FormatText),
		log.WithDestination(&buf),
		tint.ProvideTextHandler(),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.InfoContext(t.Context(), "served", slog.String("route", "/users"))

	app.RequireStop()

	require.NotEmpty(t, buf.String())
	assert.Contains(t, buf.String(), " INF served route=/users")
	assert.NotContains(t, buf.String(), "level=INFO", "the record went through tint, not slog's text handler")
}
