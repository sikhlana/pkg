package kernelrun_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
)

// recorder is a slog handler that keeps every record it's handed, at every level.
type recorder struct {
	mu      sync.Mutex
	records []record
}

// record is a record a recorder was handed, with each attribute's value as a string.
type record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

func (*recorder) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	attrs := make(map[string]string)

	rec.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()

		return true
	})

	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = append(r.records, record{Level: rec.Level, Message: rec.Message, Attrs: attrs})

	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler {
	return r
}

func (r *recorder) WithGroup(string) slog.Handler {
	return r
}

// all returns every record handed to the recorder so far.
func (r *recorder) all() []record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.records)
}

// find returns the first record with message, and whether there was one.
func (r *recorder) find(message string) (record, bool) {
	for _, rec := range r.all() {
		if rec.Message == message {
			return rec, true
		}
	}

	return record{}, false
}

// withRecorder supplies a logger over a new recorder and returns the recorder.
func withRecorder() (fx.Option, *recorder) {
	rec := &recorder{}

	return fx.Supply(slog.New(rec)), rec
}

// run runs an app built from opts under t's context, with fx's own logging off.
func run(t *testing.T, o kernelrun.Options, opts ...fx.Option) error {
	t.Helper()

	return runContext(t.Context(), o, opts...)
}

// runContext runs an app built from opts under ctx, with fx's own logging off.
func runContext(ctx context.Context, o kernelrun.Options, opts ...fx.Option) error {
	return kernelrun.Run(ctx, o, append([]fx.Option{fx.NopLogger}, opts...)...)
}

// waitDone is a runner that waits for its context and returns its error.
func waitDone(ctx context.Context) error {
	<-ctx.Done()

	return ctx.Err()
}

// returnNil is a runner that returns at once.
func returnNil(context.Context) error {
	return nil
}

// hooks registers an OnStart and an OnStop hook. Either can be nil.
func hooks(onStart, onStop func(context.Context) error) fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStart: onStart, OnStop: onStop})
	})
}

// codeError is an error type a caller defines, carrying an exit code.
type codeError struct {
	code int
}

func (e *codeError) Error() string {
	return "exit code"
}

// ctxKey is the type of the context key tests attach a value under.
type ctxKey struct{}
