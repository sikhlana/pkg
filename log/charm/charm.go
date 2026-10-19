// Package charm renders log's text format through a charm logger, with timestamps and groups written as qualified keys.
// ProvideTextHandler makes it the text handler of an app that lists log.Module.
package charm

import (
	"context"
	"io"
	"log/slog"
	"math"
	"slices"

	charmlog "charm.land/log/v2"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/log"
)

// minLabelled and maxLabelled bound the levels that New labels with slog's name for them.
const (
	minLabelled = slog.LevelDebug - 8
	maxLabelled = slog.LevelError + 8
)

// New returns a handler that renders records with a charm logger writing to w, with timestamps, the level in opts and
// the source location when opts.AddSource is set. It ignores opts.ReplaceAttr. It's a log.TextHandlerProvider.
//
// Unlike charm's own slog support, which turns groups into a prefix on the message, attributes inside groups are
// written under dot-qualified keys, as slog's text handler writes them: http.status=200. A level between the standard
// ones is labelled as slog names it, such as DEBUG-4, INFO+2 or ERROR+4, in the colour of the standard level below it,
// from Debug-8 to Error+8; further out, it takes the nearest standard level's label. Charm decides on colour itself,
// and honours NO_COLOR.
func New(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	var o slog.HandlerOptions

	if opts != nil {
		o = *opts
	}

	logger := charmlog.NewWithOptions(w, charmlog.Options{
		ReportTimestamp: true,
		ReportCaller:    o.AddSource,
		// Enabled checks o.Level on every record, so a slog.LevelVar keeps working; charm passes everything on.
		Level: charmlog.Level(math.MinInt32),
	})

	// Charm reads its styles without a lock, so every label is set here, before the logger is shared.
	logger.SetStyles(levelStyles())

	return &handler{logger: logger, level: o.Level}
}

// ProvideTextHandler supplies New as the log module's text handler.
func ProvideTextHandler() fx.Option {
	return log.WithTextHandler(New)
}

// handler flattens each record's attributes into dot-qualified keys, then hands the record to the charm logger.
type handler struct {
	logger *charmlog.Logger
	level  slog.Leveler

	// prefix is the open groups, each followed by a dot.
	prefix string

	// attrs holds the attributes given to WithAttrs, already flattened.
	attrs []slog.Attr
}

// Enabled reports whether level is at or above the handler's level, Info when none was given.
func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	minimum := slog.LevelInfo

	if h.level != nil {
		minimum = h.level.Level()
	}

	return level >= minimum
}

// Handle writes r with its attributes flattened, keeping its time, message and source location, and its level unless
// that's outside the labelled range.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := slices.Clip(h.attrs)

	r.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, h.prefix, a)

		return true
	})

	level := r.Level

	if level < minLabelled || level > maxLabelled {
		level = slog.Level(standard(level))
	}

	out := slog.NewRecord(r.Time, level, r.Message, r.PC)
	out.AddAttrs(attrs...)

	return h.logger.Handle(ctx, out)
}

// WithAttrs returns a handler that writes attrs, flattened under the open groups, on every record.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = slices.Clip(h.attrs)

	for _, a := range attrs {
		c.attrs = flatten(c.attrs, h.prefix, a)
	}

	return &c
}

// WithGroup returns a handler that qualifies every attribute given after it with name. An empty name returns the
// handler itself.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	c := *h
	c.prefix = h.prefix + name + "."

	return &c
}

// levelStyles returns charm's default styles with a label for every level from minLabelled to maxLabelled that isn't
// a standard one: slog's name for it, in the style of the standard level below it, without the padding that would
// wrap a longer label. That replaces charm's FATAL at 12, which slog calls ERROR+4.
func levelStyles() *charmlog.Styles {
	styles := charmlog.DefaultStyles()

	for l := minLabelled; l <= maxLabelled; l++ {
		base := standard(l)

		if charmlog.Level(l) != base {
			styles.Levels[charmlog.Level(l)] = styles.Levels[base].SetString(l.String()).UnsetWidth()
		}
	}

	return styles
}

// standard returns the standard level at or below l, or Debug when l is below Debug.
func standard(l slog.Level) charmlog.Level {
	switch {
	case l >= slog.LevelError:
		return charmlog.ErrorLevel
	case l >= slog.LevelWarn:
		return charmlog.WarnLevel
	case l >= slog.LevelInfo:
		return charmlog.InfoLevel
	default:
		return charmlog.DebugLevel
	}
}

// flatten appends a to dst under prefix, resolved, with a group's attributes appended one by one under the group's
// key. Empty attributes and empty groups append nothing, and a group with an empty key is inlined.
func flatten(dst []slog.Attr, prefix string, a slog.Attr) []slog.Attr {
	a.Value = a.Value.Resolve()

	if a.Equal(slog.Attr{}) {
		return dst
	}

	if a.Value.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}

		for _, member := range a.Value.Group() {
			dst = flatten(dst, prefix, member)
		}

		return dst
	}

	a.Key = prefix + a.Key

	return append(dst, a)
}
