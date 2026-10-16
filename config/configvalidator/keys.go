package configvalidator

import (
	"encoding"
	"reflect"
	"strings"
)

var textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()

// KeySegment returns the segment struct field f adds to a config key, and false when f has no key at all because it's
// unexported or tagged mapstructure:"-". The segment is the name in f's mapstructure tag, or f's Go name when the tag
// has none. A field tagged with the squash option returns an empty segment, since its fields sit at its parent's
// key. Other tag options, such as omitempty, change nothing.
//
// These are the rules every key in an *Error follows, and config/configloader uses them for env names, defaults and
// its unknown-key check, so the two can't disagree.
func KeySegment(f reflect.StructField) (string, bool) {
	if !f.IsExported() {
		return "", false
	}

	name, options, _ := strings.Cut(f.Tag.Get("mapstructure"), ",")

	if name == "-" {
		return "", false
	}

	for option := range strings.SplitSeq(options, ",") {
		if option == "squash" {
			return "", true
		}
	}

	if name == "" {
		return f.Name, true
	}

	return name, true
}

// DecodesText reports whether t, or a pointer to t, implements encoding.TextUnmarshaler. Such a type is a single
// config value however it's built: time.Time, netip.Prefix and slog.Level are, and so is a pointer to one.
// Validation doesn't search inside it, and config/configloader decodes it from its text form.
func DecodesText(t reflect.Type) bool {
	return t.Implements(textUnmarshaler) || reflect.PointerTo(t).Implements(textUnmarshaler)
}

// fieldSegment returns f's key segment, or its Go name when it has no key. Only the validator reaches such a field,
// through a value built in code rather than decoded, and the Go name still tells the developer which field it was.
func fieldSegment(f reflect.StructField) string {
	if segment, ok := KeySegment(f); ok {
		return segment
	}

	return f.Name
}

// joinKey appends segment to key. An empty side adds nothing, and a segment that starts with an index joins without a
// dot, so a child's "[2]" under "hosts" becomes "hosts[2]".
func joinKey(key, segment string) string {
	switch {
	case key == "":
		return segment
	case segment == "":
		return key
	case segment[0] == '[':
		return key + segment
	default:
		return key + "." + segment
	}
}

// within reports whether key is base or sits beneath it. Every key sits beneath the empty root key.
func within(key, base string) bool {
	if base == "" || key == base {
		return true
	}

	rest, ok := strings.CutPrefix(key, base)

	return ok && (rest[0] == '.' || rest[0] == '[')
}
