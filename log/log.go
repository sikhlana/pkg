package log

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strconv"

	"go.uber.org/fx"
)

// ErrUnknownFormat reports a Format other than FormatJSON and FormatText, from decoding a name or from New.
var ErrUnknownFormat = errors.New("unknown log format")

// Format selects how records are written. Its zero value is FormatJSON, so an unset format writes JSON.
type Format int

const (
	// FormatJSON writes each record as a JSON object on its own line, with its source location.
	FormatJSON Format = iota

	// FormatText writes each record through the text handler, or slog's text handler without one.
	FormatText
)

// String returns the format's name, json or text, or Format(n) for any other value.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatText:
		return "text"
	}

	return "Format(" + strconv.Itoa(int(f)) + ")"
}

// MarshalText returns the format's name, json or text. Any other value fails with ErrUnknownFormat.
func (f Format) MarshalText() ([]byte, error) {
	switch f {
	case FormatJSON, FormatText:
		return []byte(f.String()), nil
	}

	return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, f)
}

// UnmarshalText sets the format from its name, json or text, so a flag can parse it with pflag's TextVar. Any other
// name fails with ErrUnknownFormat and leaves the format as it was.
func (f *Format) UnmarshalText(text []byte) error {
	switch string(text) {
	case "json":
		*f = FormatJSON
	case "text":
		*f = FormatText
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, text)
	}

	return nil
}

// TextHandlerProvider builds the handler that renders FormatText records, writing to w with the level and source
// setting in opts. log/tint and log/charm each export one as New.
type TextHandlerProvider func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

// Params holds New's inputs, all optional. With fx, each one is supplied with its WithX helper, or AsX for a value
// built in the graph, and middleware with WithMiddleware or ProvideMiddleware.
type Params struct {
	fx.In

	// Debug writes records from Debug up, and asks the text handler for the source location. It wins over Quiet.
	Debug bool `name:"log:debug" optional:"true"`

	// Quiet writes records from Warn up.
	Quiet bool `name:"log:quiet" optional:"true"`

	// Format picks JSON or text output.
	Format Format `name:"log:format" optional:"true"`

	// Destination receives every record. It's standard error when nil, or a nil pointer, map, slice, channel or
	// function of a writer type.
	Destination io.Writer `name:"log:destination" optional:"true"`

	// TextHandler renders FormatText records. Without one, they go through slog's text handler, without colour. It's
	// never called for FormatJSON.
	TextHandler TextHandlerProvider `name:"log:text_handler" optional:"true"`

	// Middleware runs on every enabled record, lowest Order first. Entries without a Middleware are skipped.
	Middleware []OrderedMiddleware `group:"log:middleware"`
}

// New builds the logger. Neither Debug nor Quiet logs from Info up. JSON records always carry their source location;
// text records carry it only under Debug. The level is enforced before the text handler sees a record, so one that
// ignores the level it's given can't write records below it.
//
// New returns ErrUnknownFormat when Format is neither FormatJSON nor FormatText.
func New(p Params) (*slog.Logger, error) {
	level := slog.LevelInfo

	switch {
	case p.Debug:
		level = slog.LevelDebug
	case p.Quiet:
		level = slog.LevelWarn
	}

	w := p.Destination

	if isNil(w) {
		w = os.Stderr
	}

	var base slog.Handler

	switch p.Format {
	case FormatJSON:
		base = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, AddSource: true})
	case FormatText:
		opts := &slog.HandlerOptions{Level: level, AddSource: p.Debug}

		if p.TextHandler != nil {
			base = p.TextHandler(w, opts)
		} else {
			base = slog.NewTextHandler(w, opts)
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, p.Format)
	}

	return slog.New(newHandler(level, base, sortMiddleware(p.Middleware))), nil
}

// nilableKinds are the kinds whose values can be nil.
var nilableKinds = []reflect.Kind{
	reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer,
}

// isNil reports whether w is nil, or a nil value of a kind that can be, which nothing can be written through.
func isNil(w io.Writer) bool {
	if w == nil {
		return true
	}

	v := reflect.ValueOf(w)

	return slices.Contains(nilableKinds, v.Kind()) && v.IsNil()
}

// sortMiddleware returns the middleware ordered by Order, keeping the given order between equal ones.
func sortMiddleware(middleware []OrderedMiddleware) []Middleware {
	sorted := slices.SortedStableFunc(slices.Values(middleware), func(a, b OrderedMiddleware) int {
		return cmp.Compare(a.Order, b.Order)
	})

	out := make([]Middleware, 0, len(sorted))

	for _, m := range sorted {
		if m.Middleware != nil {
			out = append(out, m.Middleware)
		}
	}

	return out
}
