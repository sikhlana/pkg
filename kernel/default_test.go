package kernel_test

import (
	"context"
	"io"
	stdlog "log" //nolint:depguard // The tests check what Run does to the log package's output.
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/kernel"
	"github.com/sikhlana/pkg/kernelrun"
)

// saveDefault returns slog's default logger and the log package's output and flags, and restores them, with slog's
// level for the log package, when t ends.
func saveDefault(t *testing.T) (*slog.Logger, io.Writer, int) {
	t.Helper()

	logger, writer, flags, level := slog.Default(), stdlog.Writer(), stdlog.Flags(), bridgeLevel()

	t.Cleanup(func() {
		slog.SetDefault(logger)
		stdlog.SetOutput(writer)
		stdlog.SetFlags(flags)
		slog.SetLogLoggerLevel(level)
	})

	return logger, writer, flags
}

// bridgeLevel returns slog's level for what the log package writes, leaving it as it was.
func bridgeLevel() slog.Level {
	level := slog.SetLogLoggerLevel(slog.LevelInfo)
	slog.SetLogLoggerLevel(level)

	return level
}

func TestLogPackageSurvivesQuiet(t *testing.T) {
	saveDefault(t)

	root, out := program(kernel.Options{}, kernelrun.WithRunner("app", func(context.Context) error {
		stdlog.Print("http: TLS handshake error")

		return nil
	}))

	code, _ := execute(t, root, "run", "-q")

	require.Equal(t, 0, code)
	assert.Contains(t, out.messages(t, "WARN"), "http: TLS handshake error")
}

// assertLogOutput checks that the log package writes to writer with flags.
func assertLogOutput(t *testing.T, writer io.Writer, flags int) {
	t.Helper()

	assert.Equal(t, writer, stdlog.Writer(), "the log package's output wasn't restored")
	assert.Equal(t, flags, stdlog.Flags(), "the log package's flags weren't restored")
}
