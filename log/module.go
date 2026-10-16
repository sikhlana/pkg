package log

import (
	"io"

	"go.uber.org/fx"
)

// MiddlewareGroup is the fx group tag New collects OrderedMiddleware from. WithMiddleware and ProvideMiddleware
// register into it.
const MiddlewareGroup = `group:"log:middleware"`

// builtMiddleware names a ProvideMiddleware constructor's result inside the private module that pairs it with its
// order. builtParams carries the same tag.
const builtMiddleware = `name:"log:middleware:built"`

// builtParams is the input of the adapter ProvideMiddleware registers to pair a built Middleware with its order.
type builtParams struct {
	fx.In

	Middleware Middleware `name:"log:middleware:built"`
}

// Module provides the app's *slog.Logger, built by New from whatever wiring the app supplies, and a WriterProvider
// whose writers log through it.
var Module = fx.Module("log", fx.Provide(New, NewWriterProvider))

// AsDebug annotates v, a bool or a constructor returning one, as New's Debug switch. Wrap it in fx.Supply or
// fx.Provide.
func AsDebug(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"log:debug"`))
}

// WithDebug supplies New's Debug switch.
func WithDebug(debug bool) fx.Option {
	return fx.Supply(AsDebug(debug))
}

// AsQuiet annotates v, a bool or a constructor returning one, as New's Quiet switch. Wrap it in fx.Supply or
// fx.Provide.
func AsQuiet(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"log:quiet"`))
}

// WithQuiet supplies New's Quiet switch.
func WithQuiet(quiet bool) fx.Option {
	return fx.Supply(AsQuiet(quiet))
}

// AsFormat annotates v, a Format or a constructor returning one, as New's Format. Wrap it in fx.Supply or fx.Provide.
func AsFormat(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"log:format"`))
}

// WithFormat supplies New's Format.
func WithFormat(format Format) fx.Option {
	return fx.Supply(AsFormat(format))
}

// AsDestination annotates v, a writer or a constructor returning one of any concrete writer type, as New's
// Destination. Wrap it in fx.Supply or fx.Provide.
func AsDestination(v any) any {
	return fx.Annotate(v, fx.As(new(io.Writer)), fx.ResultTags(`name:"log:destination"`))
}

// WithDestination supplies New's Destination. A nil w, or a nil pointer of a writer type, supplies nothing, so New
// writes to standard error.
func WithDestination(w io.Writer) fx.Option {
	if isNil(w) {
		return fx.Options()
	}

	return fx.Supply(AsDestination(w))
}

// AsTextHandler annotates v as New's TextHandler. With fx.Provide, v is a constructor returning a
// TextHandlerProvider. With fx.Supply, v must already have the TextHandlerProvider type, since fx would otherwise take
// a function with the provider's signature for a constructor; WithTextHandler converts it for you.
func AsTextHandler(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"log:text_handler"`))
}

// WithTextHandler supplies New's TextHandler.
func WithTextHandler(provider TextHandlerProvider) fx.Option {
	return fx.Supply(AsTextHandler(provider))
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
