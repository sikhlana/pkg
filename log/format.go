package log

import (
	"errors"
	"fmt"
	"strconv"
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
