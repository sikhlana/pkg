package configvalidator

import (
	"cmp"
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

var (
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	textMarshaler   = reflect.TypeFor[encoding.TextMarshaler]()
)

// KeySegment returns the segment struct field f adds to a config key, and false when f has no key at all because it's
// tagged mapstructure:"-" or it's unexported. The segment is the name in f's mapstructure tag, or f's Go name when the
// tag has none. A field tagged with the squash option returns an empty segment, since its fields sit at its parent's
// key; that holds for an unexported embedded struct too, whose exported fields mapstructure still decodes. Other tag
// options, such as omitempty, change nothing.
//
// These are the rules every key in an *Error follows, and config/configloader uses them for env names, defaults and
// its unknown-key check, so the two can't disagree.
func KeySegment(f reflect.StructField) (string, bool) {
	name, options, _ := strings.Cut(f.Tag.Get("mapstructure"), ",")

	if name == "-" {
		return "", false
	}

	squash := slices.Contains(strings.Split(options, ","), "squash")

	switch {
	case squash && (f.IsExported() || f.Anonymous):
		return "", true
	case !f.IsExported():
		return "", false
	case name == "":
		return f.Name, true
	default:
		return name, true
	}
}

// DecodesText reports whether t, or a pointer to t, implements encoding.TextUnmarshaler. Such a type is a single
// config value however it's built: time.Time, netip.Prefix and slog.Level are, and so is a pointer to one.
// Validation doesn't search inside it, and config/configloader decodes it from its text form.
func DecodesText(t reflect.Type) bool {
	return t.Implements(textUnmarshaler) || reflect.PointerTo(t).Implements(textUnmarshaler)
}

// segmentKind says how a segment is written in a key and how it's ordered against its siblings.
type segmentKind uint8

const (
	// fieldKind is a struct field, written ".name" and ordered by declaration.
	fieldKind segmentKind = iota

	// squashKind is a squashed struct field. It adds nothing to the key, but keeps its fields apart from their
	// siblings', so a failure beside it isn't mistaken for one beneath it.
	squashKind

	// indexKind is a slice or array element, written "[i]" and ordered by index.
	indexKind

	// entryKind is a map entry, written ".name" whatever the name holds and ordered by name.
	entryKind

	// rawKind is a key a Validator wrote into an *Error itself. It's written as it is, after a dot unless it starts
	// with "[".
	rawKind
)

// segment is one step from a value to a value beneath it.
type segment struct {
	kind segmentKind

	// name is a field's key segment or Go name, an entry's name, or a raw key.
	name string

	// index is a field's position in its struct, or an element's index.
	index int
}

// path locates a value beneath a config's root. It stays a list of segments until it's written as a key, so an entry
// name holding dots or brackets never has to be parsed back out of a string.
type path []segment

// child returns p followed by s, without sharing p's backing array.
func (p path) child(s segment) path {
	return append(slices.Clip(p), s)
}

// join returns p followed by q, without sharing p's backing array.
func (p path) join(q path) path {
	return append(slices.Clip(p), q...)
}

// within reports whether p is base or lies beneath it.
func (p path) within(base path) bool {
	return len(p) >= len(base) && slices.Equal(p[:len(base)], base)
}

// String writes p as the dotted key an operator would write: "http.port", "hosts[2]" or "databases.reporting".
func (p path) String() string {
	var b strings.Builder

	for _, s := range p {
		switch s.kind {
		case squashKind:
			continue
		case indexKind:
			b.WriteString("[" + strconv.Itoa(s.index) + "]")
		case fieldKind, entryKind, rawKind:
			if b.Len() > 0 && (s.kind != rawKind || !strings.HasPrefix(s.name, "[")) {
				b.WriteByte('.')
			}

			b.WriteString(s.name)
		}
	}

	return b.String()
}

// comparePaths orders paths the way the walk visits them: fields by declaration, elements by index and entries by
// name, with a path before the paths beneath it.
func comparePaths(a, b path) int {
	for i := range min(len(a), len(b)) {
		c := cmp.Or(
			cmp.Compare(a[i].index, b[i].index),
			cmp.Compare(a[i].name, b[i].name),
			cmp.Compare(a[i].kind, b[i].kind),
		)

		if c != 0 {
			return c
		}
	}

	return cmp.Compare(len(a), len(b))
}

// fieldSegment returns the segment struct field f adds to a path. A field without a key gets its Go name: only the
// validator reaches one, through a value built in code rather than decoded, and the Go name still tells the developer
// which field it was.
func fieldSegment(f reflect.StructField) segment {
	index := f.Index[len(f.Index)-1]
	name, ok := KeySegment(f)

	switch {
	case !ok:
		return segment{kind: fieldKind, name: f.Name, index: index}
	case name == "":
		return segment{kind: squashKind, name: f.Name, index: index}
	default:
		return segment{kind: fieldKind, name: name, index: index}
	}
}

// reachable reports whether the walk enters struct field f: it's exported, or embedded, since an unexported embedded
// struct's exported fields are promoted into its parent.
func reachable(f reflect.StructField) bool {
	return f.IsExported() || f.Anonymous
}

// entry is a map entry, named as a config file names it.
type entry struct {
	name  string
	value reflect.Value
}

// sortedEntries returns map v's entries in the order of their names.
func sortedEntries(v reflect.Value) []entry {
	entries := make([]entry, 0, v.Len())

	for k, value := range v.Seq2() {
		entries = append(entries, entry{name: entryName(k), value: value})
	}

	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Compare(a.name, b.name)
	})

	return entries
}

// entryName returns the name a config file gives map key k: its text form when its type has one, and otherwise its
// underlying value, so a String method written for display can't stand in for the key the operator wrote.
func entryName(k reflect.Value) string {
	if k.Kind() == reflect.Interface && !k.IsNil() {
		return entryName(k.Elem())
	}

	if k.CanInterface() && k.Type().Implements(textMarshaler) {
		marshaler, _ := reflect.TypeAssert[encoding.TextMarshaler](k)

		if text, err := marshaler.MarshalText(); err == nil {
			return string(text)
		}
	}

	switch k.Kind() { //nolint:exhaustive // Every other kind is written in its default format.
	case reflect.String:
		return k.String()
	case reflect.Bool:
		return strconv.FormatBool(k.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(k.Float(), 'g', -1, k.Type().Bits())
	default:
		return fmt.Sprint(k)
	}
}
