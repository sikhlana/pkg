package configloader

import (
	"log/slog"

	"go.uber.org/fx"
)

// Params is what Provide's constructor takes from the graph.
type Params struct {
	fx.In

	// Filename is the config file AsFilename or WithFilename supplied. It wins over Options.Filename; empty falls
	// back to it.
	Filename string `name:"configloader:filename" optional:"true"`

	// Logger is the app's logger, such as the one log.Module provides. It wins over Options.Logger.
	Logger *slog.Logger `optional:"true"`
}

// Provide loads a T with Load and provides it to the graph, once. The file comes from Params.Filename, then
// o.Filename, then the search, and a failed load fails the app's start with the load error. When T embeds fx.Out, fx
// provides each of its fields instead, so sections of the app's config reach the modules that take them.
func Provide[T any](o Options[T]) fx.Option {
	return fx.Module("configloader", fx.Provide(func(p Params) (T, error) {
		// A copy, so an option reused in another app doesn't inherit this one's wiring.
		opts := o

		if p.Filename != "" {
			opts.Filename = p.Filename
		}

		if p.Logger != nil {
			opts.Logger = p.Logger
		}

		return Load(opts)
	}))
}

// AsFilename annotates v, a string or a constructor returning one, as the config file Provide reads. Wrap it in
// fx.Supply or fx.Provide.
func AsFilename(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"configloader:filename"`))
}

// WithFilename supplies the config file Provide reads, such as the value of a --config flag. Empty falls back to
// Options.Filename, then the search.
func WithFilename(path string) fx.Option {
	return fx.Supply(AsFilename(path))
}
