package kernel_test

import (
	"io"
	stdlog "log" //nolint:depguard // The tests check that Run restores the log package's output.
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// saveDefault returns slog's default logger and the log package's output and flags, and restores them when t ends.
func saveDefault(t *testing.T) (*slog.Logger, io.Writer, int) {
	t.Helper()

	logger, writer, flags := slog.Default(), stdlog.Writer(), stdlog.Flags()

	t.Cleanup(func() {
		slog.SetDefault(logger)
		stdlog.SetOutput(writer)
		stdlog.SetFlags(flags)
	})

	return logger, writer, flags
}

// assertLogOutput checks that the log package writes to writer with flags.
func assertLogOutput(t *testing.T, writer io.Writer, flags int) {
	t.Helper()

	assert.Equal(t, writer, stdlog.Writer(), "the log package's output wasn't restored")
	assert.Equal(t, flags, stdlog.Flags(), "the log package's flags weren't restored")
}
