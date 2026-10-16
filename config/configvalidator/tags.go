package configvalidator

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// rules evaluates validate tags. It caches what it learns about each struct type and is safe for concurrent use.
var rules = validator.New(validator.WithRequiredStructEnabled())

// checkTags evaluates the validate tags on v and everything reachable from it, and returns an *Error with a *RuleError
// cause for each failure, in the order of their keys. A struct is evaluated whole. A slice, array or map has no fields
// of its own, so each element is checked the same way under its index or entry, which is how ValidateField checks a
// slice or map field. Nothing is returned for a nil value, a type that decodes itself from text, or any other kind:
// there are no fields to evaluate.
func checkTags(v reflect.Value) []*Error {
	var c tagChecker

	c.check(v, nil)

	slices.SortStableFunc(c.violations, func(a, b *Error) int {
		return comparePaths(a.at, b.at)
	})

	return c.violations
}

// tagChecker collects the tag violations beneath one root.
type tagChecker struct {
	violations []*Error

	// entries holds each map's entries by the name go-playground prints for their keys, so a map is indexed once
	// however many of its entries fail.
	entries map[uintptr]map[string]entry
}

// check evaluates the tags on v, whose path is at.
func (c *tagChecker) check(v reflect.Value, at path) {
	v, ok := indirect(v)

	if !ok || DecodesText(v.Type()) {
		return
	}

	switch v.Kind() { //nolint:exhaustive // Only structs have tags, and only containers hold structs.
	case reflect.Struct:
		c.checkStruct(v, at)
	case reflect.Slice, reflect.Array:
		if !containerKind(v.Type().Elem().Kind()) {
			return
		}

		for i := range v.Len() {
			c.check(v.Index(i), at.child(segment{kind: indexKind, index: i}))
		}
	case reflect.Map:
		if !containerKind(v.Type().Elem().Kind()) {
			return
		}

		for _, e := range sortedEntries(v) {
			c.check(e.value, at.child(segment{kind: entryKind, name: e.name}))
		}
	}
}

// checkStruct evaluates the tags on struct v, whose path is at, and everything reachable from it.
func (c *tagChecker) checkStruct(v reflect.Value, at path) {
	err := rules.Struct(v.Interface())
	if err == nil {
		return
	}

	failures, ok := errors.AsType[validator.ValidationErrors](err)

	if !ok {
		c.violations = append(c.violations, newError(at, err))

		return
	}

	for _, failure := range failures {
		c.violations = append(c.violations, newError(
			at.join(c.resolve(v, failure.StructNamespace())),
			&RuleError{Tag: failure.Tag(), Param: failure.Param()},
		))
	}
}

// resolve translates the Go-name namespace go-playground reports for a failure under root, such as
// "Config.Hosts[2].Addr", into the path it names. It follows the namespace through root's value, so squashed fields
// become squash segments, interface fields are seen through, and a map entry's name is matched against the map's
// keys. If the namespace can't be followed, the part after the root is kept as one raw segment.
func (c *tagChecker) resolve(root reflect.Value, namespace string) path {
	// An anonymous root struct has no name, and its namespace starts at the first field.
	rest := strings.TrimPrefix(namespace, root.Type().Name())

	if at, ok := c.follow(root, rest, nil); ok {
		return at
	}

	return path{{kind: rawKind, name: strings.TrimPrefix(rest, ".")}}
}

// follow returns at extended by the path the namespace rest names beneath v, and false when rest doesn't fit v.
func (c *tagChecker) follow(v reflect.Value, rest string, at path) (path, bool) {
	if rest == "" {
		return at, true
	}

	v, ok := indirect(v)

	if !ok {
		return nil, false
	}

	switch v.Kind() { //nolint:exhaustive // A namespace only continues through containers.
	case reflect.Struct:
		return c.followField(v, rest, at)
	case reflect.Slice, reflect.Array:
		return c.followIndex(v, rest, at)
	case reflect.Map:
		return c.followEntry(v, rest, at)
	default:
		return nil, false
	}
}

// followField follows ".Name", or "Name" at the start of an anonymous root's namespace, where Name is one of struct
// v's own fields.
func (c *tagChecker) followField(v reflect.Value, rest string, at path) (path, bool) {
	rest = strings.TrimPrefix(rest, ".")

	end := strings.IndexAny(rest, ".[")

	if end < 0 {
		end = len(rest)
	}

	for f, field := range v.Fields() {
		if f.Name == rest[:end] {
			return c.follow(field, rest[end:], at.child(fieldSegment(f)))
		}
	}

	return nil, false
}

// followIndex follows "[i]", where i is an index of slice or array v.
func (c *tagChecker) followIndex(v reflect.Value, rest string, at path) (path, bool) {
	end := strings.IndexByte(rest, ']')

	if !strings.HasPrefix(rest, "[") || end < 0 {
		return nil, false
	}

	i, err := strconv.Atoi(rest[1:end])

	if err != nil || i < 0 || i >= v.Len() {
		return nil, false
	}

	return c.follow(v.Index(i), rest[end+1:], at.child(segment{kind: indexKind, index: i}))
}

// followEntry follows "[name]", where name is how go-playground prints one of map v's keys. A name can itself hold
// "]", so each "]" is tried in turn until the entry it closes exists and the rest of the namespace fits beneath it.
func (c *tagChecker) followEntry(v reflect.Value, rest string, at path) (path, bool) {
	if !strings.HasPrefix(rest, "[") {
		return nil, false
	}

	entries := c.index(v)

	for end := 1; end < len(rest); end++ {
		if rest[end] != ']' {
			continue
		}

		e, ok := entries[rest[1:end]]

		if !ok {
			continue
		}

		if found, fits := c.follow(e.value, rest[end+1:], at.child(segment{kind: entryKind, name: e.name})); fits {
			return found, true
		}
	}

	return nil, false
}

// index returns map v's entries by the name go-playground prints for their keys, building it on first use.
func (c *tagChecker) index(v reflect.Value) map[string]entry {
	if entries, ok := c.entries[v.Pointer()]; ok {
		return entries
	}

	entries := make(map[string]entry, v.Len())

	for k, value := range v.Seq2() {
		entries[fmt.Sprint(k)] = entry{name: entryName(k), value: value}
	}

	if c.entries == nil {
		c.entries = make(map[uintptr]map[string]entry)
	}

	c.entries[v.Pointer()] = entries

	return entries
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
