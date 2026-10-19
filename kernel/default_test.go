package kernel_test

import (
	"context"
	"io"
	stdlog "log" //nolint:depguard // The tests check what Run does to the log package's output.
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

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

func TestDefaultLoggerDuringTheRun(t *testing.T) {
	root, out := program(kernel.Options{}, kernelrun.WithRunner("app", func(ctx context.Context) error {
		//nolint:sloglint // The test checks that slog's default logger writes through the app's logger.
		slog.InfoContext(ctx, "through the default")

		return nil
	}))

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Contains(t, out.messages(t, "INFO"), "through the default")
}

func TestDefaultLoggerAheadOfOtherModules(t *testing.T) {
	early := fx.Module("early", fx.Invoke(func() {
		//nolint:sloglint // The test checks that a module's invoke already writes through the app's logger.
		slog.Info("from a module's invoke")
	}))

	root, out := program(kernel.Options{Shared: early}, kernelrun.WithRunner("app", returnNil))

	code, _ := execute(t, root, "run")

	require.Equal(t, 0, code)
	assert.Contains(t, out.messages(t, "INFO"), "from a module's invoke")
}

func TestDefaultLoggerRestored(t *testing.T) {
	tests := []struct {
		name string
		opts []fx.Option
		code int
	}{
		{name: "after a clean run", opts: []fx.Option{kernelrun.WithRunner("app", returnNil)}},
		{name: "after a startup failure", opts: []fx.Option{failingConstructor()}, code: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, writer, flags := saveDefault(t)
			level := bridgeLevel()

			root, _ := program(kernel.Options{}, tt.opts...)

			code, _ := execute(t, root, "run")

			require.Equal(t, tt.code, code)
			assert.Same(t, logger, slog.Default())
			assertLogOutput(t, writer, flags)
			assert.Equal(t, level, bridgeLevel(), "the log package's level wasn't restored")
		})
	}
}

// assertLogOutput checks that the log package writes to writer with flags.
func assertLogOutput(t *testing.T, writer io.Writer, flags int) {
	t.Helper()

	assert.Equal(t, writer, stdlog.Writer(), "the log package's output wasn't restored")
	assert.Equal(t, flags, stdlog.Flags(), "the log package's flags weren't restored")
}
