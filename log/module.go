package log

import (
	"io"

	"go.uber.org/fx"
)

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
