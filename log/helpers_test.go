package log_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/log"
)

// recording is a text handler provider whose handlers keep every record they're handed, and the arguments each
// provider call got. Its handlers accept every level, so they show whether the core handler enforces the level.
type recording struct {
	t *testing.T

	mu      sync.Mutex
	writers []io.Writer
	opts    []*slog.HandlerOptions
	records []recorded
}

// recorded is a record a recording handler was handed. Attrs is fmt.Sprint of its attributes, so it shows their order
// and nesting.
type recorded struct {
	Level   slog.Level
	Message string
	PC      uintptr
	Attrs   string
}

// newRecorded builds a text-format logger from p over a recording.
func newRecorded(t *testing.T, p log.Params) (*slog.Logger, *recording) {
	t.Helper()

	rec := &recording{t: t}
	p.Format = log.FormatText
	p.TextHandler = rec.provide

	logger, err := log.New(p)
	require.NoError(t, err)

	return logger, rec
}

func (rec *recording) provide(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	rec.writers = append(rec.writers, w)
	rec.opts = append(rec.opts, opts)

	return recordingHandler{rec: rec}
}

// all returns every record handed to the recording's handlers so far.
func (rec *recording) all() []recorded {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	return slices.Clone(rec.records)
}

// attrs returns the Attrs of every record handed to the recording's handlers so far.
func (rec *recording) attrs() []string {
	var out []string

	for _, r := range rec.all() {
		out = append(out, r.Attrs)
	}

	return out
}

type recordingHandler struct {
	rec *recording
}

func (recordingHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	var attrs []slog.Attr

	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)

		return true
	})

	h.rec.mu.Lock()
	defer h.rec.mu.Unlock()

	h.rec.records = append(h.rec.records, recorded{
		Level:   r.Level,
		Message: r.Message,
		PC:      r.PC,
		Attrs:   fmt.Sprint(attrs),
	})

	return nil
}

func (h recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	assert.Fail(h.rec.t, "the core handler forwarded attributes to its base", "attributes %v", attrs)

	return h
}

func (h recordingHandler) WithGroup(name string) slog.Handler {
	assert.Fail(h.rec.t, "the core handler forwarded a group to its base", "group %q", name)

	return h
}

// decodeLines decodes each line in b as a JSON object.
func decodeLines(t *testing.T, b []byte) []map[string]any {
	t.Helper()

	var out []map[string]any

	scanner := bufio.NewScanner(bytes.NewReader(b))

	for scanner.Scan() {
		var m map[string]any

		require.NoError(t, json.Unmarshal(scanner.Bytes(), &m), scanner.Text())

		out = append(out, m)
	}

	require.NoError(t, scanner.Err())

	return out
}

// values returns each object's value for key.
func values(objects []map[string]any, key string) []any {
	out := make([]any, 0, len(objects))

	for _, m := range objects {
		out = append(out, m[key])
	}

	return out
}

type requestIDKey struct{}

// addRequestID is a middleware that adds the context's request id at the top level.
func addRequestID(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}

	return next(ctx, r)
}

// adding returns a middleware that adds attrs to every record.
func adding(attrs ...slog.Attr) log.MiddlewareFunc {
	return func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
		r.AddAttrs(attrs...)

		return next(ctx, r)
	}
}

// counting returns a middleware that counts the records it sees and passes them on.
func counting(n *int) log.MiddlewareFunc {
	return func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
		*n++

		return next(ctx, r)
	}
}

// tracing returns a middleware that appends name to seen and passes the record on.
func tracing(seen *[]string, name string) log.MiddlewareFunc {
	return func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
		*seen = append(*seen, name)

		return next(ctx, r)
	}
}

// declared is a middleware that declares its order.
type declared struct {
	log.MiddlewareFunc

	order int
}

func (d declared) Order() int {
	return d.order
}

func withRequestID(t *testing.T, id string) context.Context {
	t.Helper()

	return context.WithValue(t.Context(), requestIDKey{}, id)
}
