package log

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"go.uber.org/fx"
)

// Middleware runs on every enabled record, with the context it was logged with. It passes the record on by calling
// next, changed or not, and drops it by returning without calling next. Attributes it adds land at the top level of
// the record, even when the caller opened a group, and a group it adds merges with any top-level group of the same
// key. next is the rest of the chain after this middleware, built once in New, so it's the same for every record and
// safe for concurrent use.
//
// The record shares its attribute storage with the caller's copy. A middleware that keeps it, or passes it to next
// more than once, clones it first with slog.Record.Clone.
//
// The method's signature uses only standard-library types, so a package can implement Middleware without importing
// log. It clashes with slog.Handler's Handle, so no type is both a handler and a middleware.
type Middleware interface {
	Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error
}

// OrderedMiddleware is a Middleware that declares where it runs in the chain. Lower orders run first, nearest the
// caller, and see the record before higher ones do; a Middleware that isn't an OrderedMiddleware counts as order 0.
// Middleware with equal orders run in an unspecified relative order, which can change from one run to the next.
type OrderedMiddleware interface {
	Middleware
	Order() int
}

// MiddlewareFunc adapts a function to a Middleware. It declares no order, so it runs at 0 unless wrapped with
// Ordered.
type MiddlewareFunc func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error

// ordered is the OrderedMiddleware Ordered returns.
type ordered struct {
	Middleware

	order int
}

// Handle calls f.
func (f MiddlewareFunc) Handle(
	ctx context.Context,
	r slog.Record,
	next func(context.Context, slog.Record) error,
) error {
	return f(ctx, r, next)
}

// Order returns the order Ordered was given.
func (o ordered) Order() int {
	return o.order
}

// Ordered returns m at order, replacing any order m declares itself, so an app can place middleware it didn't write.
func Ordered(order int, m Middleware) OrderedMiddleware {
	if o, ok := m.(ordered); ok {
		m = o.Middleware
	}

	return ordered{Middleware: m, order: order}
}

// chain returns a function that runs the record through middleware in order, then merges its groups and hands it to
// base.
func chain(middleware []Middleware, base slog.Handler) func(context.Context, slog.Record) error {
	next := func(ctx context.Context, r slog.Record) error {
		return base.Handle(ctx, merge(r))
	}

	for _, m := range slices.Backward(middleware) {
		inner := next

		next = func(ctx context.Context, r slog.Record) error {
			return m.Handle(ctx, r, inner)
		}
	}

	return next
}

// sortMiddleware returns the middleware that isn't absent, lowest order first, keeping the given order between equal
// ones.
func sortMiddleware(middleware []Middleware) []Middleware {
	sorted := slices.DeleteFunc(slices.Clone(middleware), absent)

	slices.SortStableFunc(sorted, func(a, b Middleware) int {
		return cmp.Compare(orderOf(a), orderOf(b))
	})

	return sorted
}

// absent reports whether m stands for no middleware: nil, a nil pointer, func, map, slice or channel behind the
// interface, or one of those wrapped by Ordered.
func absent(m Middleware) bool {
	if o, ok := m.(ordered); ok {
		return absent(o.Middleware)
	}

	return isNil(m)
}

// orderOf returns m's declared order, or 0 when it declares none.
func orderOf(m Middleware) int {
	if o, ok := m.(OrderedMiddleware); ok {
		return o.Order()
	}

	return 0
}

// MiddlewareGroup is the fx group tag New collects Middleware from. WithMiddleware and ProvideMiddleware register
// into it.
const MiddlewareGroup = `group:"log:middleware"`

// WithMiddleware adds m to the middleware chain, at the order it declares. Wrap it with Ordered to place it
// elsewhere. A nil m supplies nothing.
func WithMiddleware(m Middleware) fx.Option {
	if m == nil {
		return fx.Options()
	}

	return fx.Supply(fx.Annotate(m, fx.As(new(Middleware)), fx.ResultTags(MiddlewareGroup)))
}

// ProvideMiddleware adds the middleware constructor builds from the graph to the chain, at the order it declares.
// constructor can take any parameters fx can supply, and returns any type that implements Middleware, optionally
// followed by an error. A constructor that returns a nil Middleware, as New describes it, leaves its middleware out,
// for a condition known only when it runs.
func ProvideMiddleware(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.As(new(Middleware)), fx.ResultTags(MiddlewareGroup)))
}
