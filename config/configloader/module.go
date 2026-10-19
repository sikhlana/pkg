package configloader

import (
	"go.uber.org/fx"
)

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
