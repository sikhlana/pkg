package log

import "errors"

// ErrUnknownFormat reports a Format other than FormatJSON and FormatText, from decoding a name or from New.
var ErrUnknownFormat = errors.New("unknown log format")
