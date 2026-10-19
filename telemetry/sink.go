package telemetry

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.uber.org/fx"
)

// otelWarn is the slog level OpenTelemetry's warnings reach a logr.FromSlogHandler handler at: they're logged at
// logr verbosity 1, which it maps to level -1.
const otelWarn = slog.Level(-1)

var (
	// installsMu guards installs.
	installsMu sync.Mutex

	// installs holds every installed logger, oldest first. The last one is current.
	installs []*install

	// current is the last entry in installs, nil while it's empty, read without the lock by forwarders.
	current atomic.Pointer[install]

	// fallback handles records while no logger is installed: errors and warnings go to standard error, as
	// OpenTelemetry's own defaults send them, rather than being dropped.
	fallback slog.Handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})
)

// install is one InstallErrorSink call. Its address identifies it, so one logger can be installed twice.
type install struct {
	logger *slog.Logger
}

// InstallErrorSink sends OpenTelemetry's own errors and log messages to logger: errors reported through its error
// handler (failed exports, among others) and its warnings at Warn, an error carrying the error, errors its internal
// logger reports at Error, and its informational and debug messages below slog.LevelInfo. The SDK's exporters and
// processors report failures through nothing else. OpenTelemetry's handler and logger are process-wide, so they
// forward to the most recently installed logger that hasn't been uninstalled, and while there's none, send errors
// and warnings to standard error, as OpenTelemetry's defaults do. A nil logger discards.
//
// The returned uninstall function removes logger, so records go back to the logger installed before it if that one
// is still installed. Calling it again does nothing.
func InstallErrorSink(logger *slog.Logger) func() {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	forward := slog.New(forwarder{})

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		forward.LogAttrs(context.Background(), slog.LevelWarn, "opentelemetry error", slog.Any("error", err))
	}))

	otel.SetLogger(logr.FromSlogHandler(forwarder{fromLogr: true}))

	entry := &install{logger: logger}

	installsMu.Lock()

	installs = append(installs, entry)
	current.Store(entry)
	installsMu.Unlock()

	var once sync.Once

	return func() {
		once.Do(func() { uninstall(entry) })
	}
}

// uninstall removes entry from installs and makes the last remaining one current.
func uninstall(entry *install) {
	installsMu.Lock()
	defer installsMu.Unlock()

	installs = slices.DeleteFunc(installs, func(e *install) bool { return e == entry })

	if len(installs) == 0 {
		current.Store(nil)

		return
	}

	current.Store(installs[len(installs)-1])
}

// ErrorSink is the error sink Module installs while the app runs. A signal's constructor takes it, so its stop hooks
// run before the sink is uninstalled and errors from its final flush still reach the logger.
type ErrorSink struct{}

// ErrorSinkParams holds NewErrorSink's inputs.
type ErrorSinkParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *slog.Logger
}

// NewErrorSink appends a hook that installs Logger with InstallErrorSink on start and uninstalls it on stop. A hook
// appended after it, by anything that depends on the ErrorSink, starts after it and stops before it.
func NewErrorSink(p ErrorSinkParams) ErrorSink {
	var uninstallSink func()

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			uninstallSink = InstallErrorSink(p.Logger)

			return nil
		},
		OnStop: func(context.Context) error {
			uninstallSink()

			return nil
		},
	})

	return ErrorSink{}
}

// forwarder is a slog.Handler that hands each record to the current logger's handler, or to fallback while no logger
// is installed, with the attributes and groups it collected applied in order. One that serves logr raises
// OpenTelemetry's warnings to slog.LevelWarn.
type forwarder struct {
	ops      []func(slog.Handler) slog.Handler
	built    *atomic.Pointer[builtHandler]
	fromLogr bool
}

// builtHandler is a logger's handler with a forwarder's attributes and groups applied.
type builtHandler struct {
	install *install
	handler slog.Handler
}

func (f forwarder) Enabled(ctx context.Context, level slog.Level) bool {
	return f.handler().Enabled(ctx, f.level(level))
}

func (f forwarder) Handle(ctx context.Context, r slog.Record) error {
	r.Level = f.level(r.Level)

	return f.handler().Handle(ctx, r)
}

func (f forwarder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return f.with(func(h slog.Handler) slog.Handler { return h.WithAttrs(attrs) })
}

func (f forwarder) WithGroup(name string) slog.Handler {
	return f.with(func(h slog.Handler) slog.Handler { return h.WithGroup(name) })
}

// with returns a forwarder that applies op after f's own, with a cache of its own.
func (f forwarder) with(op func(slog.Handler) slog.Handler) forwarder {
	return forwarder{
		ops:      append(slices.Clip(f.ops), op),
		built:    new(atomic.Pointer[builtHandler]),
		fromLogr: f.fromLogr,
	}
}

// level returns the level a record at l is handed on at.
func (f forwarder) level(l slog.Level) slog.Level {
	if f.fromLogr && l == otelWarn {
		return slog.LevelWarn
	}

	return l
}

// handler returns the current logger's handler, or fallback when no logger is installed, with f's attributes and
// groups applied. The result is cached until another logger becomes current, since a handler may do real work in
// WithAttrs.
func (f forwarder) handler() slog.Handler {
	entry := current.Load()

	base := fallback

	if entry != nil {
		base = entry.logger.Handler()
	}

	if len(f.ops) == 0 {
		return base
	}

	if b := f.built.Load(); b != nil && b.install == entry {
		return b.handler
	}

	h := base

	for _, op := range f.ops {
		h = op(h)
	}

	f.built.Store(&builtHandler{install: entry, handler: h})

	return h
}
