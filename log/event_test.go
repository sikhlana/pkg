package log_test

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

var errHook = errors.New("hook failed")

// eventApp returns an app that sends fx's events through the module's logger to buf, with a start hook that returns
// startErr.
func eventApp(t *testing.T, buf *bytes.Buffer, startErr error, opts ...fx.Option) *fxtest.App {
	t.Helper()

	return fxtest.New(t,
		log.Module,
		log.WithDestination(buf),
		fx.Options(opts...),
		fx.WithLogger(log.NewEventLogger),
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.StartStopHook(
				func() error { return startErr },
				func() error { return nil },
			))
		}),
	)
}

func TestEventLoggerHidesEventsByDefault(t *testing.T) {
	var buf bytes.Buffer

	eventApp(t, &buf, nil).RequireStart().RequireStop()

	assert.Empty(t, buf.String())
}

func TestEventLoggerShowsEventsUnderDebug(t *testing.T) {
	var buf bytes.Buffer

	eventApp(t, &buf, nil, log.WithDebug(true)).RequireStart().RequireStop()

	lines := decodeLines(t, buf.Bytes())
	msgs := values(lines, slog.MessageKey)

	assert.Contains(t, msgs, "provided")
	assert.Contains(t, msgs, "invoking")
	assert.Contains(t, msgs, "OnStart hook executed")
	assert.Contains(t, msgs, "OnStop hook executed")
	assert.Equal(t, []any{"DEBUG"}, slices.Compact(values(lines, slog.LevelKey)), "every event is at Debug")
}

func TestEventLoggerShowsAFailureUnderQuiet(t *testing.T) {
	var buf bytes.Buffer

	app := eventApp(t, &buf, errHook, log.WithQuiet(true))

	require.ErrorIs(t, app.Start(t.Context()), errHook)

	var failures []any

	for _, line := range decodeLines(t, buf.Bytes()) {
		if line[slog.LevelKey] == "ERROR" {
			failures = append(failures, line[slog.MessageKey])
		}
	}

	assert.Contains(t, failures, "OnStart hook failed")
}
