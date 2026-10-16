package log_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

// childEnv is set when the test binary re-runs itself to check what reaches standard error.
const childEnv = "LOG_TEST_CHILD"

func TestLevel(t *testing.T) {
	tests := []struct {
		name string
		opts []fx.Option
		want []any
	}{
		{
			name: "neither switch",
			want: []any{"INFO", "WARN", "ERROR"},
		},
		{
			name: "debug",
			opts: []fx.Option{log.WithDebug(true)},
			want: []any{"DEBUG", "INFO", "WARN", "ERROR"},
		},
		{
			name: "quiet",
			opts: []fx.Option{log.WithQuiet(true)},
			want: []any{"WARN", "ERROR"},
		},
		{
			name: "both switches",
			opts: []fx.Option{log.WithDebug(true), log.WithQuiet(true)},
			want: []any{"DEBUG", "INFO", "WARN", "ERROR"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				buf    bytes.Buffer
				logger *slog.Logger
			)

			app := fxtest.New(t,
				log.Module,
				log.WithDestination(&buf),
				fx.Options(tt.opts...),
				fx.Populate(&logger),
			)
			app.RequireStart()

			for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				logger.LogAttrs(t.Context(), level, "logged")
			}

			app.RequireStop()

			assert.Equal(t, tt.want, values(decodeLines(t, buf.Bytes()), slog.LevelKey))
		})
	}
}

func TestLevelHoldsOverATextHandlerThatIgnoresIt(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})

	logger.DebugContext(t.Context(), "hidden")

	assert.Empty(t, rec.all())
}

func TestOutputDefaultsToStandardError(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		logger, err := log.New(log.Params{})
		require.NoError(t, err)

		logger.InfoContext(t.Context(), "to standard error")

		return
	}

	lines := decodeLines(t, runChild(t, "TestOutputDefaultsToStandardError"))

	require.Len(t, lines, 1)
	assert.NotEmpty(t, lines[0][slog.TimeKey])
	assert.Equal(t, "INFO", lines[0][slog.LevelKey])
	assert.Equal(t, "to standard error", lines[0][slog.MessageKey])
	assert.NotEmpty(t, lines[0][slog.SourceKey])
}

func TestOutputGoesToASuppliedWriter(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		var buf bytes.Buffer

		logger, err := log.New(log.Params{Destination: &buf})
		require.NoError(t, err)

		logger.InfoContext(t.Context(), "to the writer")

		lines := decodeLines(t, buf.Bytes())
		require.Len(t, lines, 1)
		assert.Equal(t, "to the writer", lines[0][slog.MessageKey])

		return
	}

	assert.Empty(t, runChild(t, "TestOutputGoesToASuppliedWriter"))
}

func TestANilDestinationMeansStandardError(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		for _, dest := range []io.Writer{nil, (*os.File)(nil)} {
			var logger *slog.Logger

			app := fxtest.New(t, log.Module, log.WithDestination(dest), fx.Populate(&logger))
			app.RequireStart()

			logger.InfoContext(t.Context(), "supplied", slog.Bool("typed", dest != nil))

			app.RequireStop()
		}

		logger, err := log.New(log.Params{Destination: (*bytes.Buffer)(nil)})
		require.NoError(t, err)

		logger.InfoContext(t.Context(), "filled in")

		return
	}

	lines := decodeLines(t, runChild(t, "TestANilDestinationMeansStandardError"))

	assert.Equal(t, []any{"supplied", "supplied", "filled in"}, values(lines, slog.MessageKey))
	assert.Equal(t, []any{false, true, nil}, values(lines, "typed"))
}

func TestTextHandlerIsBuiltWithTheDestinationAndLevel(t *testing.T) {
	var buf bytes.Buffer

	logger, rec := newRecorded(t, log.Params{Destination: &buf, Quiet: true})

	logger.WarnContext(t.Context(), "rendered")

	require.Len(t, rec.writers, 1)
	assert.Same(t, &buf, rec.writers[0])
	assert.Equal(t, slog.LevelWarn, rec.opts[0].Level.Level())
	assert.Equal(t, []string{"rendered"}, messages(rec.all()))
}

func TestTextFallsBackToSlogsTextHandler(t *testing.T) {
	var buf bytes.Buffer

	logger, err := log.New(log.Params{Format: log.FormatText, Destination: &buf})
	require.NoError(t, err)

	logger.InfoContext(t.Context(), "served", slog.String("route", "/users"))

	assert.Contains(t, buf.String(), "level=INFO msg=served route=/users\n")
	assert.NotContains(t, buf.String(), "\x1b[")
}

func TestJSONNeverBuildsTheTextHandler(t *testing.T) {
	var (
		buf   bytes.Buffer
		built bool
	)

	logger, err := log.New(log.Params{
		Destination: &buf,
		TextHandler: func(io.Writer, *slog.HandlerOptions) slog.Handler {
			built = true

			return slog.DiscardHandler
		},
	})
	require.NoError(t, err)

	logger.InfoContext(t.Context(), "as json")

	assert.False(t, built)
	assert.Equal(t, []any{"as json"}, values(decodeLines(t, buf.Bytes()), slog.MessageKey))
}

func TestJSONCarriesTheSourceLocation(t *testing.T) {
	var buf bytes.Buffer

	logger, err := log.New(log.Params{Destination: &buf})
	require.NoError(t, err)

	_, file, line, _ := runtime.Caller(0)
	logger.InfoContext(t.Context(), "located", slog.Int("want_line", line+1))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)

	source, ok := lines[0][slog.SourceKey].(map[string]any)
	require.True(t, ok, "source is an object")
	assert.Equal(t, file, source["file"])
	assert.Equal(t, lines[0]["want_line"], source["line"])
}

func TestTextAsksForTheSourceOnlyUnderDebug(t *testing.T) {
	for _, debug := range []bool{false, true} {
		_, rec := newRecorded(t, log.Params{Debug: debug})

		require.Len(t, rec.opts, 1)
		assert.Equal(t, debug, rec.opts[0].AddSource, "debug %t", debug)
	}
}

func TestARecordWithoutASourceCarriesNone(t *testing.T) {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "unlocated", 0)

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer

		logger, err := log.New(log.Params{Destination: &buf})
		require.NoError(t, err)

		require.NoError(t, logger.Handler().Handle(t.Context(), r))

		lines := decodeLines(t, buf.Bytes())
		require.Len(t, lines, 1)
		assert.NotContains(t, lines[0], slog.SourceKey)
	})

	t.Run("text", func(t *testing.T) {
		var buf bytes.Buffer

		logger, err := log.New(log.Params{Format: log.FormatText, Debug: true, Destination: &buf})
		require.NoError(t, err)

		require.NoError(t, logger.Handler().Handle(t.Context(), r))

		assert.Contains(t, buf.String(), "msg=unlocated")
		assert.NotContains(t, buf.String(), slog.SourceKey+"=")
	})
}

// runChild re-runs the test binary for the named test with childEnv set, and returns what it wrote to standard error.
func runChild(t *testing.T, test string) []byte {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+test+"$")

	cmd.Env = append(os.Environ(), childEnv+"=1")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "stdout:\n%s\nstderr:\n%s", &stdout, &stderr)
	require.Contains(t, stdout.String(), "PASS", "the child ran %s", test)

	return stderr.Bytes()
}

// messages returns each record's message.
func messages(records []recorded) []string {
	out := make([]string, 0, len(records))

	for _, r := range records {
		out = append(out, r.Message)
	}

	return out
}
