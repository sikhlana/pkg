package log

import (
	"context"
	"log/slog"
	"slices"

	"go.uber.org/fx"
)

// Middleware runs on every enabled record, with the context it was logged with. It passes the record on by calling
// next, changed or not, and drops it by returning without calling next. Attributes it adds land at the top level of
// the record, even when the caller opened a group, and a group it adds merges with any top-level group of the same
// key.
//
// The record shares its attribute storage with the caller's copy. A middleware that keeps it, or passes it to next
// more than once, clones it first with slog.Record.Clone.
//
// The signature uses only standard-library types, so a package can export a matching function without importing log.
type Middleware func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error

// OrderedMiddleware pairs a Middleware with its place in the chain. Lower orders run first, nearest the caller, and
// see the record before higher ones do. Middleware with equal orders run in an unspecified order, which can change
// from one run to the next.
type OrderedMiddleware struct {
	Order      int
	Middleware Middleware
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
			return m(ctx, r, inner)
		}
	}

	return next
}

// MiddlewareGroup is the fx group tag New collects OrderedMiddleware from. WithMiddleware and ProvideMiddleware
// register into it.
const MiddlewareGroup = `group:"log:middleware"`

// builtMiddleware names a ProvideMiddleware constructor's result inside the private module that pairs it with its
// order. builtParams carries the same tag.
const builtMiddleware = `name:"log:middleware::built"`

// builtParams is the input of the adapter ProvideMiddleware registers to pair a built Middleware with its order.
type builtParams struct {
	fx.In

	Middleware Middleware `name:"log:middleware::built"`
}

// WithMiddleware registers m at order in the middleware chain.
func WithMiddleware(order int, m Middleware) fx.Option {
	return fx.Supply(fx.Annotate(OrderedMiddleware{Order: order, Middleware: m}, fx.ResultTags(MiddlewareGroup)))
}

// ProvideMiddleware registers the Middleware that constructor builds from the graph at order in the middleware chain.
// The constructor can take any parameters fx can supply, and its first result must have the Middleware type.
func ProvideMiddleware(order int, constructor any) fx.Option {
	return fx.Module("log",
		fx.Provide(fx.Private, fx.Annotate(constructor, fx.ResultTags(builtMiddleware))),
		fx.Provide(fx.Annotate(
			func(p builtParams) OrderedMiddleware {
				return OrderedMiddleware{Order: order, Middleware: p.Middleware}
			},
			fx.ResultTags(MiddlewareGroup),
		)),
	)
}
