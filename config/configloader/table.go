package configloader

import (
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/sikhlana/pkg/config/configvalidator"
)

// fileSuffix ends the name of the env variable that names a file holding a key's value.
const fileSuffix = "_FILE"

var textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()

var (
	// ErrEnvNameCollision is returned when two keys would share an env name, or one key's env name is another's
	// _FILE name. The error names both keys and the name.
	ErrEnvNameCollision = errors.New("env name collision")

	// ErrUnknownKeys is returned when the config file sets keys that match no field and Options.AllowUnknownKeys
	// isn't set. The error lists every such key in full, sorted.
	ErrUnknownKeys = errors.New("unknown config keys")
)

// leaf is a key that holds a single value, with the env name it's read from.
type leaf struct {
	key string
	env string

	// def is the key's default, or invalid when Options.Defaults doesn't reach it.
	def reflect.Value

	// nested reports whether the key can hold content beneath it, as a field of interface type can.
	nested bool
}

// table holds every key a load knows, in the order the walk over the config type finds them: fields by declaration,
// map entries by name.
type table struct {
	leaves []leaf

	// sections are the keys of structs and maps.
	sections map[string]bool

	// open are the keys of structs with a ,remain field, which take any key beneath them. The root's key is empty.
	open map[string]bool
}

// newTable walks t, the config type, with configvalidator's key rules, giving every leaf an env name under prefix. A
// map field takes its entries from the file's keys and from defaults, the value of Options.Defaults.
// It fails when the config type or defaults hold something that can't be loaded.
func newTable(prefix string, t reflect.Type, defaults reflect.Value, fileKeys []string) (*table, error) {
	b := &tableBuilder{
		prefix:   prefix,
		fileKeys: fileKeys,
		table:    &table{sections: map[string]bool{}, open: map[string]bool{}},
		active:   map[reflect.Type]bool{},
	}

	b.walk(t, defaults, "")

	if b.err != nil {
		return nil, b.err
	}

	return b.table, nil
}

// checkCollisions fails with ErrEnvNameCollision when two keys would read the same env variable.
func (t *table) checkCollisions() error {
	owners := map[string]string{}

	for _, l := range t.leaves {
		for _, name := range []string{l.env, l.env + fileSuffix} {
			if other, ok := owners[name]; ok && other != l.key {
				return fmt.Errorf("%w: %s and %s both use %s", ErrEnvNameCollision, other, l.key, name)
			}

			owners[name] = l.key
		}
	}

	return nil
}

// unknown returns the keys among fileKeys that the table doesn't know, sorted.
func (t *table) unknown(fileKeys []string) []string {
	leaves := map[string]leaf{}

	for _, l := range t.leaves {
		leaves[l.key] = l
	}

	var unknown []string

	for _, key := range fileKeys {
		if !t.knows(key, leaves) {
			unknown = append(unknown, key)
		}
	}

	slices.Sort(unknown)

	return unknown
}

// knows reports whether key is a leaf or a section, or lies beneath a leaf that holds nested content or a struct
// with a ,remain field.
func (t *table) knows(key string, leaves map[string]leaf) bool {
	if _, ok := leaves[key]; ok || t.sections[key] || t.open[""] {
		return true
	}

	for i := range len(key) {
		if key[i] != '.' {
			continue
		}

		ancestor := key[:i]

		if l, ok := leaves[ancestor]; ok && l.nested || t.open[ancestor] {
			return true
		}
	}

	return false
}

// checkUnknownKeys fails with ErrUnknownKeys when fileKeys holds keys the table doesn't know, or logs each at Warn when
// allow is set.
func (l *loader) checkUnknownKeys(t *table, fileKeys []string, allow bool) error {
	unknown := t.unknown(fileKeys)

	if len(unknown) == 0 {
		return nil
	}

	if !allow {
		return fmt.Errorf("%w: %s", ErrUnknownKeys, strings.Join(unknown, ", "))
	}

	for _, key := range unknown {
		l.logger.Warn("ignoring unknown config key", slog.String("key", key))
	}

	return nil
}

// tableBuilder fills a table by walking a config type and its defaults side by side.
type tableBuilder struct {
	*table

	prefix   string
	fileKeys []string

	// active holds the struct types the walk is inside, so a type that contains itself doesn't recurse forever.
	active map[reflect.Type]bool

	// err is the first problem the walk found.
	err error
}

// walk adds the leaves of a value of type t at key, whose default is def.
func (b *tableBuilder) walk(t reflect.Type, def reflect.Value, key string) {
	for t.Kind() == reflect.Pointer && !configvalidator.DecodesText(t) {
		t, def = t.Elem(), elem(def)
	}

	switch {
	case configvalidator.DecodesText(t):
		b.addLeaf(t, def, key)
	case t.Kind() == reflect.Struct && b.active[t]:
		// A type that contains itself has no end to its keys, so nothing beneath this one gets an env name, and any
		// file key beneath it is left for decoding to check.
		b.sections[key] = true
		b.open[key] = true
	case t.Kind() == reflect.Struct:
		b.sections[key] = true
		b.active[t] = true
		b.walkFields(t, def, key)
		delete(b.active, t)
	case t.Kind() == reflect.Map:
		b.sections[key] = true
		b.walkEntries(t, def, key)
	default:
		b.addLeaf(t, def, key)
	}
}

// walkFields adds the leaves of struct type t's fields beneath key.
func (b *tableBuilder) walkFields(t reflect.Type, def reflect.Value, key string) {
	for i := range t.NumField() {
		f := t.Field(i)

		segment, ok := configvalidator.KeySegment(f)

		if !ok {
			continue
		}

		if !f.IsExported() && f.Type.Kind() == reflect.Pointer {
			// mapstructure panics allocating a squashed embed it can't set.
			b.fail(fmt.Errorf("field %s of %s: an unexported embedded pointer can't be decoded", f.Name, t))

			continue
		}

		if hasOption(f, "remain") {
			b.open[key] = true

			continue
		}

		var fieldDef reflect.Value

		if def.IsValid() {
			fieldDef = def.Field(i)
		}

		b.walk(f.Type, fieldDef, joinKey(key, strings.ToLower(segment)))
	}
}

// walkEntries adds the leaves of each entry of map type t at key that the file or def defines.
func (b *tableBuilder) walkEntries(t reflect.Type, def reflect.Value, key string) {
	defs := map[string]reflect.Value{}
	originals := map[string]string{}

	if def.IsValid() {
		for k, v := range def.Seq2() {
			original := entryName(k)
			name := strings.ToLower(original)

			switch other, ok := originals[name]; {
			case name == "" || strings.Contains(name, "."):
				b.fail(fmt.Errorf("default entry %q of %s: an entry name can't be empty or hold a dot", original, key))
			case ok:
				first, second := min(other, original), max(other, original)
				b.fail(fmt.Errorf("%w: default entries %s and %s both load as %s",
					ErrEnvNameCollision, joinKey(key, first), joinKey(key, second), joinKey(key, name)))
			}

			defs[name] = v
			originals[name] = original
		}
	}

	names := make([]string, 0, len(defs))

	for name := range defs {
		names = append(names, name)
	}

	prefix := key + "."

	if key == "" {
		prefix = ""
	}

	for _, fileKey := range b.fileKeys {
		if rest, ok := strings.CutPrefix(fileKey, prefix); ok {
			name, _, _ := strings.Cut(rest, ".")
			names = append(names, name)
		}
	}

	slices.Sort(names)

	for _, name := range slices.Compact(names) {
		b.walk(t.Elem(), defs[name], joinKey(key, name))
	}
}

// fail records err, unless the walk already failed.
func (b *tableBuilder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// addLeaf adds key, of type t, as a leaf whose default is def.
func (b *tableBuilder) addLeaf(t reflect.Type, def reflect.Value, key string) {
	if key == "" {
		return
	}

	if def.IsValid() && (def.Kind() == reflect.Pointer || def.Kind() == reflect.Interface) && def.IsNil() {
		def = reflect.Value{}
	}

	if def.IsValid() && !def.CanInterface() {
		b.fail(fmt.Errorf("default for %s: a value reached through an unexported field can't be read", key))

		return
	}

	b.leaves = append(b.leaves, leaf{
		key:    key,
		env:    envName(b.prefix, key),
		def:    def,
		nested: t.Kind() == reflect.Interface,
	})
}

// envName returns key's env name: prefix, _, and key uppercased with every character other than ASCII letters,
// digits and _ replaced by _.
func envName(prefix, key string) string {
	// Replaced before uppercasing, since uppercasing some characters outside ASCII yields ASCII letters: ß gives SS.
	return prefix + "_" + strings.ToUpper(strings.Map(func(r rune) rune {
		if !isEnvRune(r) {
			return '_'
		}

		return r
	}, key))
}

// joinKey returns key followed by segment, with a dot between them when both are non-empty.
func joinKey(key, segment string) string {
	switch {
	case key == "":
		return segment
	case segment == "":
		return key
	default:
		return key + "." + segment
	}
}

// hasOption reports whether struct field f's mapstructure tag carries option, as in ",remain".
func hasOption(f reflect.StructField, option string) bool {
	_, options, _ := strings.Cut(f.Tag.Get("mapstructure"), ",")

	return slices.Contains(strings.Split(options, ","), option)
}

// elem returns what pointer or interface v holds, or an invalid value when v is invalid or nil.
func elem(v reflect.Value) reflect.Value {
	if !v.IsValid() || v.IsNil() {
		return reflect.Value{}
	}

	return v.Elem()
}

// entryName returns the name a config file gives map key k, by config/configvalidator's rule: its text form when its
// type has one, and otherwise its underlying value, so a String method written for display can't stand in for the key
// the operator wrote. A key reached through an unexported field is named by its underlying value.
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
