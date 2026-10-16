package log

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/fx"
)

// maxLine is the longest run without a newline that Writer writes as one record.
const maxLine = 64 << 10

// Writer writes each line written to it as a record, for a subprocess's output and the like. Create one with
// NewWriter, or with the WriterProvider the module provides. It's safe for concurrent use.
type Writer struct {
	mu      sync.Mutex
	ctx     context.Context
	level   slog.Level
	handler slog.Handler
	pc      uintptr
	buf     []byte
}

// NewWriter returns a Writer that logs each line through logger at level, with ctx, so middleware sees ctx. Every
// record carries the source location of the NewWriter call, not of whatever calls Write. Close it to write a last
// line that has no newline.
func NewWriter(ctx context.Context, level slog.Level, logger *slog.Logger) *Writer {
	return &Writer{ctx: ctx, level: level, handler: logger.Handler(), pc: callerPC()}
}

// WriterProvider returns a Writer that logs each line at level, with ctx, through the logger it was built with, and
// with attrs on every record. Every record carries the source location of the WriterProvider call.
type WriterProvider func(ctx context.Context, level slog.Level, attrs ...slog.Attr) *Writer

// WriterProviderParams holds NewWriterProvider's inputs.
type WriterProviderParams struct {
	fx.In

	// Logger is the logger every Writer logs through.
	Logger *slog.Logger
}

// NewWriterProvider returns a WriterProvider whose writers log through p.Logger. Module provides it, so a module can
// take a WriterProvider from the graph instead of the logger:
//
//	stdout := newWriter(ctx, slog.LevelInfo, slog.String("stream", "stdout"))
func NewWriterProvider(p WriterProviderParams) WriterProvider {
	return func(ctx context.Context, level slog.Level, attrs ...slog.Attr) *Writer {
		h := p.Logger.Handler()

		if len(attrs) > 0 {
			h = h.WithAttrs(attrs)
		}

		return &Writer{ctx: ctx, level: level, handler: h, pc: callerPC()}
	}
}

// callerPC returns the program counter of the caller of the function that calls it.
func callerPC() uintptr {
	var pcs [1]uintptr

	runtime.Callers(3, pcs[:])

	return pcs[0]
}

// Write logs each newline-terminated line in p as a record, without the newline or a carriage return before it, and
// buffers the rest for the next Write. Empty lines are skipped. Every run of 64 KiB without a newline is written as a
// record of its own, cut after its last complete UTF-8 character, however the writes arrive, so the buffer never
// holds more than that. When the writer's level is disabled, Write discards p.
//
// Write always reports every byte written and a nil error, whatever the logger's handler returns, so a failing log
// destination never breaks the process writing to it.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.handler.Enabled(w.ctx, w.level) {
		return len(p), nil
	}

	n := len(p)

	for {
		line, rest, found := bytes.Cut(p, []byte{'\n'})

		if !found {
			break
		}

		// Only the first line can continue a buffered one; the rest are logged straight from p.
		if len(w.buf) > 0 {
			w.buffer(line)
			line = w.buf
		}

		w.logLine(line)
		w.buf = w.buf[:0]
		p = rest
	}

	w.buffer(p)

	return n, nil
}

// Close logs any buffered partial line. The writer stays usable, and its next Close logs what's written after this
// one.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logLine(w.buf)
	w.buf = w.buf[:0]

	return nil
}

// buffer appends b, which holds no newline, to the buffered line, logging each run of it that reaches maxLine.
func (w *Writer) buffer(b []byte) {
	for len(w.buf)+len(b) >= maxLine {
		n := maxLine - len(w.buf)
		w.buf = append(w.buf, b[:n]...)
		b = b[n:]

		cut := lastRuneEnd(w.buf)
		w.log(w.buf[:cut])
		w.buf = append(w.buf[:0], w.buf[cut:]...)
	}

	w.buf = append(w.buf, b...)
}

// logLine logs line, less a trailing carriage return, in runs of at most maxLine bytes. An empty line logs nothing.
func (w *Writer) logLine(line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})

	for len(line) > maxLine {
		cut := lastRuneEnd(line[:maxLine])
		w.log(line[:cut])
		line = line[cut:]
	}

	if len(line) > 0 {
		w.log(line)
	}
}

// log hands msg to the logger's handler as a record at the writer's level and source location.
func (w *Writer) log(msg []byte) {
	r := slog.NewRecord(time.Now(), w.level, string(msg), w.pc)

	_ = w.handler.Handle(w.ctx, r)
}

// lastRuneEnd returns the length of the longest prefix of b that doesn't end partway through a UTF-8 sequence.
func lastRuneEnd(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return len(b)
			}

			return i
		}
	}

	return len(b)
}
