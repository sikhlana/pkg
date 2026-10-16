package configvalidator

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// rules evaluates validate tags. It caches what it learns about each struct type and is safe for concurrent use.
var rules = validator.New(validator.WithRequiredStructEnabled())

// checkTags evaluates the validate tags on v and everything reachable from it, and returns an *Error with a *RuleError
// cause for each failure. It returns nothing when v, after following pointers and interfaces, is nil, isn't a struct,
// or decodes itself from text: there are no fields to evaluate.
func checkTags(v reflect.Value) []*Error {
	v, ok := indirect(v)

	if !ok || v.Kind() != reflect.Struct || DecodesText(v.Type()) {
		return nil
	}

	err := rules.Struct(v.Interface())
	if err == nil {
		return nil
	}

	failures, ok := errors.AsType[validator.ValidationErrors](err)

	if !ok {
		return []*Error{{Err: err}}
	}

	violations := make([]*Error, 0, len(failures))

	for _, failure := range failures {
		violations = append(violations, &Error{
			Key: namespaceKey(v, failure.StructNamespace()),
			Err: &RuleError{Tag: failure.Tag(), Param: failure.Param()},
		})
	}

	return violations
}

// namespaceKey translates the Go-name namespace go-playground reports for a failure under root, such as
// "Config.Hosts[2].Addr", into the config key it names, such as "hosts[2].addr". It follows the namespace through
// root's value, so squashed fields drop out, interface fields are seen through, and a map entry's name is matched
// against the map's keys even when it contains dots or brackets. If the namespace stops matching the value, the rest
// of it is appended as it is.
func namespaceKey(root reflect.Value, namespace string) string {
	// An anonymous root struct has no name, and its namespace starts at the first field.
	rest := strings.TrimPrefix(namespace, root.Type().Name())
	v := root
	key := ""

	for rest != "" {
		var ok bool

		v, ok = indirect(v)

		if ok {
			switch v.Kind() { //nolint:exhaustive // A namespace only continues through containers.
			case reflect.Struct:
				key, v, rest, ok = fieldStep(key, v, rest)
			case reflect.Slice, reflect.Array:
				key, v, rest, ok = indexStep(key, v, rest)
			case reflect.Map:
				key, v, rest, ok = entryStep(key, v, rest)
			default:
				ok = false
			}
		}

		if !ok {
			return joinKey(key, strings.TrimPrefix(rest, "."))
		}
	}

	return key
}

// fieldStep consumes ".Name" or, at the start of an anonymous root's namespace, "Name" from rest, where Name is one of
// struct v's own fields, and appends that field's segment to key.
func fieldStep(key string, v reflect.Value, rest string) (string, reflect.Value, string, bool) {
	rest = strings.TrimPrefix(rest, ".")

	end := strings.IndexAny(rest, ".[")

	if end < 0 {
		end = len(rest)
	}

	for f, field := range v.Fields() {
		if f.Name == rest[:end] {
			return joinKey(key, fieldSegment(f)), field, rest[end:], true
		}
	}

	return key, v, rest, false
}

// indexStep consumes "[i]" from rest, where i is an index of slice or array v, and appends it to key.
func indexStep(key string, v reflect.Value, rest string) (string, reflect.Value, string, bool) {
	end := strings.IndexByte(rest, ']')

	if !strings.HasPrefix(rest, "[") || end < 0 {
		return key, v, rest, false
	}

	i, err := strconv.Atoi(rest[1:end])

	if err != nil || i < 0 || i >= v.Len() {
		return key, v, rest, false
	}

	return key + rest[:end+1], v.Index(i), rest[end+1:], true
}

// entryStep consumes "[entry]" from rest, where entry names one of map v's keys, and appends ".entry" to key. The
// entry ends at the first "]" that closes the name of a key v has, so names containing "]" are found too.
func entryStep(key string, v reflect.Value, rest string) (string, reflect.Value, string, bool) {
	if !strings.HasPrefix(rest, "[") {
		return key, v, rest, false
	}

	for end := 1; end < len(rest); end++ {
		if rest[end] != ']' {
			continue
		}

		for k, value := range v.Seq2() {
			if fmt.Sprint(k) == rest[1:end] {
				return entryKey(key, rest[1:end]), value, rest[end+1:], true
			}
		}
	}

	return key, v, rest, false
}

// entryKey appends a map entry's name to key with a dot, whatever the name contains.
func entryKey(key, entry string) string {
	if key == "" {
		return entry
	}

	return key + "." + entry
}

// indirect follows non-nil pointers and interfaces to the value they hold, and returns false when it meets a nil one.
func indirect(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, false
		}

		v = v.Elem()
	}

	return v, true
}
