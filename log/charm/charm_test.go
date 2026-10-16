package charm_test

import (
	"bytes"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/go-logfmt/logfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/log"
	"github.com/sikhlana/pkg/log/charm"
)

func TestGroupsAreQualifiedKeys(t *testing.T) {
	var buf bytes.Buffer

	slog.New(charm.New(&buf, nil)).WithGroup("http").InfoContext(t.Context(), "served", slog.Int("status", 200))

	assert.Regexp(t, `INFO +served http\.status=200\n`, buf.String())
	assert.NotContains(t, buf.String(), "http:")
	assert.NotContains(t, buf.String(), "\x1b[", "no colour to a buffer")
}

func TestLevelAndSource(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(charm.New(&buf, &slog.HandlerOptions{Level: slog.LevelWarn, AddSource: true}))

	logger.InfoContext(t.Context(), "hidden")
	assert.Empty(t, buf.String())

	_, _, line, _ := runtime.Caller(0)
	logger.WarnContext(t.Context(), "located", slog.Int("want_line", line+1))

	assert.Contains(t, buf.String(), "charm_test.go:"+strconv.Itoa(line+1))
	assert.Contains(t, buf.String(), "located")
}

func TestLevelsBetweenTheStandardOnesAreLabelled(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(charm.New(&buf, &slog.HandlerOptions{Level: slog.LevelDebug - 20}))

	levels := []struct {
		level slog.Level
		want  string
	}{
		{level: slog.LevelDebug - 4, want: "DEBUG-4"},
		{level: slog.LevelInfo + 2, want: "INFO+2"},
		{level: slog.LevelError + 4, want: "ERROR+4"},
		{level: slog.LevelError + 12, want: "ERROR"},
		{level: slog.LevelDebug - 12, want: "DEBUG"},
	}

	for _, l := range levels {
		logger.LogAttrs(t.Context(), l.level, "labelled")
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")

	require.Len(t, lines, len(levels), "one line per record, with no label wrapped")

	for i, l := range levels {
		assert.Contains(t, lines[i], l.want+" labelled", "level %d", l.level)
	}

	assert.NotContains(t, buf.String(), "FATAL")
}

func TestNoSourceWithoutAddSource(t *testing.T) {
	var buf bytes.Buffer

	slog.New(charm.New(&buf, nil)).InfoContext(t.Context(), "unlocated")

	assert.Contains(t, buf.String(), "unlocated")
	assert.NotContains(t, buf.String(), "charm_test.go")
}

func TestHandlerConformance(t *testing.T) {
	var buf bytes.Buffer

	slogtest.Run(t,
		func(*testing.T) slog.Handler {
			buf.Reset()

			h := charm.New(&buf, nil)
			charm.UseLogfmt(h)

			return h
		},
		func(t *testing.T) map[string]any {
			return parseLogfmt(t, buf.Bytes())
		},
	)
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
		charm.ProvideTextHandler(),
		fx.Populate(&logger),
	)
	app.RequireStart()

	logger.WithGroup("http").InfoContext(t.Context(), "served", slog.Int("status", 200))

	app.RequireStop()

	assert.Regexp(t, `INFO +served http\.status=200\n`, buf.String())
}

// parseLogfmt decodes the one logfmt record in b, nesting each dot-qualified key's value under its groups.
func parseLogfmt(t *testing.T, b []byte) map[string]any {
	t.Helper()

	out := map[string]any{}
	d := logfmt.NewDecoder(bytes.NewReader(b))

	require.True(t, d.ScanRecord(), "a record in %q", b)

	for d.ScanKeyval() {
		path := strings.Split(string(d.Key()), ".")
		group := out

		for _, name := range path[:len(path)-1] {
			inner, ok := group[name].(map[string]any)

			if !ok {
				inner = map[string]any{}
				group[name] = inner
			}

			group = inner
		}

		group[path[len(path)-1]] = string(d.Value())
	}

	require.NoError(t, d.Err())
	require.False(t, d.ScanRecord(), "only one record in %q", b)

	return out
}
