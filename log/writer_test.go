package log_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
)

var errHandle = errors.New("handle failed")

func TestWriterLogsEachLine(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{name: "two lines", writes: []string{"one\ntwo\n"}, want: []string{"one", "two"}},
		{name: "carriage returns and empty lines", writes: []string{"one\r\n\r\ntwo\n"}, want: []string{"one", "two"}},
		{name: "a line across writes", writes: []string{"o", "ne\nt", "wo\n"}, want: []string{"one", "two"}},
		{name: "a partial line on close", writes: []string{"tail"}, want: []string{"tail"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, rec := newRecorded(t, log.Params{})
			w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

			for _, s := range tt.writes {
				n, err := io.WriteString(w, s)
				require.NoError(t, err)
				assert.Equal(t, len(s), n)
			}

			require.NoError(t, w.Close())

			assert.Equal(t, tt.want, messages(rec.all()))
		})
	}
}

func TestWriterCutsALongLineAtACharacter(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})
	w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	// Three-byte characters, so 64 KiB falls partway through one.
	written := strings.Repeat("€", 100<<10/len("€"))

	_, err := io.WriteString(w, written)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	got := messages(rec.all())

	require.Greater(t, len(got), 1)

	for _, msg := range got {
		assert.True(t, utf8.ValidString(msg), "a message of %d bytes is valid UTF-8", len(msg))
		assert.LessOrEqual(t, len(msg), 64<<10)
	}

	assert.Equal(t, written, strings.Join(got, ""))
}

func TestWriterCutsALongLineHoweverItArrives(t *testing.T) {
	// Three-byte characters, so 64 KiB falls partway through one.
	written := strings.Repeat("€", 100<<10/len("€"))

	logged := func(chunk int) []string {
		logger, rec := newRecorded(t, log.Params{})
		w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

		for s := written + "\n"; s != ""; {
			n := min(chunk, len(s))

			_, err := io.WriteString(w, s[:n])
			require.NoError(t, err)

			s = s[n:]
		}

		return messages(rec.all())
	}

	whole := logged(len(written) + 1)

	require.Greater(t, len(whole), 1, "a line over 64 KiB in one write is cut")

	for _, msg := range whole {
		assert.True(t, utf8.ValidString(msg), "a message of %d bytes is valid UTF-8", len(msg))
		assert.LessOrEqual(t, len(msg), 64<<10)
	}

	assert.Equal(t, written, strings.Join(whole, ""))
	assert.Equal(t, whole, logged(32<<10), "the same records from 32 KiB writes")
	assert.Equal(t, whole, logged(1000), "the same records from 1000-byte writes")
}

func TestWriterBufferStaysSmall(t *testing.T) {
	logger, err := log.New(log.Params{Destination: io.Discard})
	require.NoError(t, err)

	lines := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	_, err = io.WriteString(lines, strings.Repeat("a short line\n", 1<<16))
	require.NoError(t, err)

	assert.Zero(t, log.BufferCap(lines), "complete lines are logged without buffering")

	run := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	_, err = io.WriteString(run, strings.Repeat("x", 1<<20))
	require.NoError(t, err)

	assert.LessOrEqual(t, log.BufferCap(run), 2*64<<10, "a long run never buffers more than 64 KiB")
}

func TestWriterAtADisabledLevel(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})
	w := log.NewWriter(t.Context(), slog.LevelDebug, logger)

	n, err := io.WriteString(w, "hidden\n")
	require.NoError(t, err)
	assert.Equal(t, len("hidden\n"), n)

	require.NoError(t, w.Close())

	assert.Empty(t, rec.all())
}

func TestWriterRecordsCarryWhereItWasCreated(t *testing.T) {
	var buf bytes.Buffer

	logger, err := log.New(log.Params{Destination: &buf})
	require.NoError(t, err)

	_, file, line, _ := runtime.Caller(0)
	w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	_, err = io.WriteString(w, "located\n")
	require.NoError(t, err)

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)

	source, ok := lines[0][slog.SourceKey].(map[string]any)
	require.True(t, ok, "source is an object")
	assert.Equal(t, file, source["file"])
	assert.InDelta(t, line+1, source["line"], 0)
}

func TestWriterProviderFromTheModule(t *testing.T) {
	var (
		buf       bytes.Buffer
		newWriter log.WriterProvider
	)

	app := fxtest.New(t,
		log.Module,
		log.WithDestination(&buf),
		log.WithMiddleware(log.MiddlewareFunc(addRequestID)),
		fx.Populate(&newWriter),
	)
	app.RequireStart()

	_, file, line, _ := runtime.Caller(0)
	w := newWriter(withRequestID(t, "r2"), slog.LevelWarn, slog.String("stream", "stderr"))

	_, err := io.WriteString(w, "provided\n")
	require.NoError(t, err)

	app.RequireStop()

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)

	assert.Equal(t, "provided", lines[0][slog.MessageKey])
	assert.Equal(t, "WARN", lines[0][slog.LevelKey])
	assert.Equal(t, "stderr", lines[0]["stream"], "the attribute given to the provider")
	assert.Equal(t, "r2", lines[0]["request_id"], "middleware saw the writer's context")

	source, ok := lines[0][slog.SourceKey].(map[string]any)
	require.True(t, ok, "source is an object")
	assert.Equal(t, file, source["file"])
	assert.InDelta(t, line+1, source["line"], 0, "the location of the provider call")
}

func TestWriterProviderWithoutAttributes(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{})
	newWriter := log.NewWriterProvider(log.WriterProviderParams{Logger: logger})

	first := newWriter(t.Context(), slog.LevelInfo)
	second := newWriter(t.Context(), slog.LevelInfo, slog.String("stream", "stdout"))

	_, err := io.WriteString(first, "plain\n")
	require.NoError(t, err)

	_, err = io.WriteString(second, "tagged\n")
	require.NoError(t, err)

	assert.Equal(t, []string{"plain", "tagged"}, messages(rec.all()))
	assert.Equal(t, []string{"[]", "[stream=stdout]"}, rec.attrs(), "attributes stay with the writer given them")
}

func TestWriterRecordsCarryItsContext(t *testing.T) {
	logger, rec := newRecorded(t, log.Params{
		Middleware: []log.Middleware{log.MiddlewareFunc(addRequestID)},
	})
	w := log.NewWriter(withRequestID(t, "r1"), slog.LevelInfo, logger)

	_, err := io.WriteString(w, "one\ntwo\n")
	require.NoError(t, err)

	assert.Equal(t, []string{"[request_id=r1]", "[request_id=r1]"}, rec.attrs())
}

func TestWriterSucceedsWhenTheHandlerFails(t *testing.T) {
	logger, err := log.New(log.Params{
		Format: log.FormatText,
		TextHandler: func(io.Writer, *slog.HandlerOptions) slog.Handler {
			return failingHandler{}
		},
	})
	require.NoError(t, err)

	w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	n, err := io.WriteString(w, "one\ntwo")
	require.NoError(t, err)
	assert.Equal(t, len("one\ntwo"), n)
	require.NoError(t, w.Close())
}

func TestWriterIsSafeForConcurrentUse(t *testing.T) {
	const writers, lines = 8, 100

	logger, rec := newRecorded(t, log.Params{})
	w := log.NewWriter(t.Context(), slog.LevelInfo, logger)

	var wg sync.WaitGroup

	for range writers {
		wg.Go(func() {
			for range lines {
				_, _ = io.WriteString(w, "line\n")
			}
		})
	}

	wg.Wait()

	got := messages(rec.all())

	assert.Len(t, got, writers*lines)

	for _, msg := range got {
		assert.Equal(t, "line", msg)
	}
}

// failingHandler accepts every record and fails to write it.
type failingHandler struct{}

func (failingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (failingHandler) Handle(context.Context, slog.Record) error { return errHandle }

func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h failingHandler) WithGroup(string) slog.Handler { return h }
