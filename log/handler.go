package log

import (
	"context"
	"log/slog"
	"slices"
)

// handler is the slog.Handler behind New's logger. It enforces the level, runs the middleware chain and merges groups
// that share a key before base writes the record.
//
// Every attribute given to WithAttrs, and every group given to WithGroup, is held and folded into each record before
// the chain runs, so the chain sees the whole record, relative to the root, and anything it adds lands at the top
// level. Base never sees WithAttrs or WithGroup, so the chain is built once.
type handler struct {
	level slog.Level
	base  slog.Handler
	chain func(context.Context, slog.Record) error

	// attrs holds the attributes given to WithAttrs before any WithGroup.
	attrs []slog.Attr

	// groups holds the open groups, outermost first, each with the attributes given after it.
	groups []group
}

// group is an open group and the attributes given to WithAttrs while it was the innermost one.
type group struct {
	name  string
	attrs []slog.Attr
}

func newHandler(level slog.Level, base slog.Handler, middleware []Middleware) *handler {
	return &handler{
		level: level,
		base:  base,
		chain: chain(middleware, base),
	}
}

// Enabled reports whether level is at or above the logger's level and base accepts it.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.base.Enabled(ctx, level)
}

// Handle folds the held groups and attributes into r, when there are any, and runs it through the chain.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if len(h.attrs) > 0 || len(h.groups) > 0 {
		r = h.fold(r)
	}

	return h.chain(ctx, r)
}

// WithAttrs returns a handler that adds attrs to every record, under the innermost open group.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	c := *h

	if len(h.groups) == 0 {
		c.attrs = slices.Concat(h.attrs, attrs)

		return &c
	}

	c.groups = slices.Clone(h.groups)
	last := &c.groups[len(c.groups)-1]
	last.attrs = slices.Concat(last.attrs, attrs)

	return &c
}

// WithGroup returns a handler that puts every attribute given after it under name. An empty name returns the handler
// itself.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	c := *h
	c.groups = append(slices.Clip(h.groups), group{name: name})

	return &c
}

// fold returns r rebuilt relative to the root: the held attributes, then the open groups nested with their attributes
// and, innermost, r's own attributes.
func (h *handler) fold(r slog.Record) slog.Record {
	attrs := recordAttrs(r)

	for _, g := range slices.Backward(h.groups) {
		attrs = []slog.Attr{slog.GroupAttrs(g.name, slices.Concat(g.attrs, attrs)...)}
	}

	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(h.attrs...)
	out.AddAttrs(attrs...)

	return out
}

// merge returns r with the groups that share a key with a sibling, at any depth, merged into the first. It returns r
// itself, without allocating, when no two sibling groups share a key.
func merge(r slog.Record) slog.Record {
	if !hasDuplicateGroups(r) {
		return r
	}

	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(mergeGroups(recordAttrs(r))...)

	return out
}

// hasDuplicateGroups reports whether two sibling groups in r share a key, at any depth.
func hasDuplicateGroups(r slog.Record) bool {
	var (
		buf       [16]string
		keys      = buf[:0]
		duplicate bool
	)

	r.Attrs(func(a slog.Attr) bool {
		if !mergeable(a) {
			return true
		}

		if slices.Contains(keys, a.Key) || duplicateGroups(a.Value.Group()) {
			duplicate = true

			return false
		}

		keys = append(keys, a.Key)

		return true
	})

	return duplicate
}

// duplicateGroups reports whether two sibling groups in attrs share a key, at any depth.
func duplicateGroups(attrs []slog.Attr) bool {
	var (
		buf  [16]string
		keys = buf[:0]
	)

	for _, a := range attrs {
		if !mergeable(a) {
			continue
		}

		if slices.Contains(keys, a.Key) || duplicateGroups(a.Value.Group()) {
			return true
		}

		keys = append(keys, a.Key)
	}

	return false
}

// mergeGroups returns attrs with the groups that share a key merged into the first, at its position, and the
// attributes of every group merged the same way.
func mergeGroups(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))

	for _, a := range attrs {
		if !mergeable(a) {
			out = append(out, a)

			continue
		}

		i := slices.IndexFunc(out, func(b slog.Attr) bool {
			return mergeable(b) && b.Key == a.Key
		})

		switch {
		case i >= 0:
			out[i] = slog.GroupAttrs(a.Key, mergeGroups(slices.Concat(out[i].Value.Group(), a.Value.Group()))...)
		case duplicateGroups(a.Value.Group()):
			out = append(out, slog.GroupAttrs(a.Key, mergeGroups(a.Value.Group())...))
		default:
			out = append(out, a)
		}
	}

	return out
}

// mergeable reports whether a is a group with a key. Empty-key groups are inlined by the handler that writes them,
// and LogValuers are resolved there, so neither is merged.
func mergeable(a slog.Attr) bool {
	return a.Key != "" && a.Value.Kind() == slog.KindGroup
}

// recordAttrs returns r's attributes in a new slice.
func recordAttrs(r slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())

	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)

		return true
	})

	return attrs
}
