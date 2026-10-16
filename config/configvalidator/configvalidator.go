package configvalidator

import (
	"cmp"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strconv"
)

// Validator is a config type with checks its validate tags can't express, such as the required fields of the active
// mode or invariants across fields. Validate calls the method on the outermost Validator in each branch of a config,
// which then owns that branch: nothing beneath it is searched for Validate methods.
//
// To check a field, return configvalidator.ValidateField(c, &c.Field), or combine several such results with errors.Join,
// which drops the nil ones.
// Each violation is then reported under the field's full key. A Validate method must not pass its own receiver to
// Validate, which would call the method again.
type Validator interface {
	Validate() error
}

// Validate checks cfg and returns every violation it finds as an Errors, or nil when there are none. Each violation
// is an *Error naming the config key it's about.
//
// It runs in two passes. First the validate tags on every exported field reachable through struct fields and non-nil
// pointers are evaluated, except beneath a field tagged validate:"-". Then cfg is walked through struct fields,
// pointers, slice and array elements, map values and interface values, again skipping validate:"-" fields, and
// Validate is called on each outermost Validator found, through its value or its address. A Validator isn't called
// when a tag failed on it or beneath it, since its checks may rely on what its tags guarantee.
//
// A type that decodes itself from text (see DecodesText) is a single value: its own Validate method is called, and
// nothing inside it is searched. Tag violations name the rule and its parameter, never the value. What a Validator
// returns is kept as it is, under its key.
//
// Validate returns ErrNilConfig when cfg is nil. It's safe for concurrent use.
func Validate[T any](cfg *T) error {
	if cfg == nil {
		return ErrNilConfig
	}

	root := reflect.ValueOf(cfg).Elem()

	var w walker

	for _, violation := range checkTags(root) {
		w.failed = append(w.failed, violation.Key)
		w.violations = append(w.violations, violation)
	}

	w.visit(root, "")

	return w.violations.orNil()
}

// ValidateField validates field, which must point to a value beneath the one parent points to, and reports each
// violation under field's key within parent. It's how a Validate method checks one of its fields: for a messaging
// config whose amqp field fails on url, ValidateField(m, &m.AMQP) reports "amqp.url", and the walk adds the method's
// own key to make "messaging.amqp.url". With Validate(&m.AMQP) instead, the "amqp" segment would be lost.
//
// field is found by its address, through struct fields, non-nil pointers and interfaces, and slice and array elements,
// including beneath fields tagged validate:"-". Parent and field come from the same receiver, so this works with value and
// pointer receivers alike.
//
// ValidateField returns ErrNilConfig when field is nil. It panics when field isn't beneath parent, since that's a bug
// in the calling method rather than a problem with the config.
func ValidateField[P, F any](parent *P, field *F) error {
	if field == nil {
		return ErrNilConfig
	}

	target := reflect.ValueOf(field)

	key, ok := findField(reflect.ValueOf(parent).Elem(), "", target)

	if !ok {
		panic(fmt.Sprintf("configvalidator: ValidateField: %T isn't a field beneath %T", field, parent))
	}

	err := Validate(field)
	if err == nil {
		return nil
	}

	return prefix(key, err).orNil()
}

// findField returns the key of the value target points to, which must sit beneath v, whose key is key. v itself is
// never a match, so a parent can't be its own field.
func findField(v reflect.Value, key string, target reflect.Value) (string, bool) {
	for childKey, child := range children(v, key) {
		if isAt(child, target) {
			return childKey, true
		}

		if found, ok := findField(child, childKey, target); ok {
			return found, true
		}
	}

	return "", false
}

// children yields the values directly beneath v whose addresses can be taken through v, with their keys: what a
// non-nil pointer or interface holds, a struct's exported fields, and a slice's or array's elements.
func children(v reflect.Value, key string) iter.Seq2[string, reflect.Value] {
	return func(yield func(string, reflect.Value) bool) {
		switch v.Kind() { //nolint:exhaustive // Only these kinds hold addressable values beneath them.
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				yield(key, v.Elem())
			}
		case reflect.Struct:
			for f, field := range v.Fields() {
				if f.IsExported() && !yield(joinKey(key, fieldSegment(f)), field) {
					return
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				if !yield(key+"["+strconv.Itoa(i)+"]", v.Index(i)) {
					return
				}
			}
		}
	}
}

// isAt reports whether v is the value target points to.
func isAt(v, target reflect.Value) bool {
	return v.CanAddr() && v.Type() == target.Type().Elem() && v.Addr().UnsafePointer() == target.UnsafePointer()
}

// walker finds the Validators in a config and collects what they return.
type walker struct {
	// failed holds the key of every tag violation.
	failed []string

	violations Errors
}

// visit calls the outermost Validators at or beneath v, whose key is key.
func (w *walker) visit(v reflect.Value, key string) {
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return
	}

	if v.Kind() == reflect.Interface {
		w.visit(addressable(v.Elem()), key)

		return
	}

	if validator, ok := asValidator(v); ok {
		w.call(validator, key)

		return
	}

	if DecodesText(v.Type()) {
		return
	}

	switch v.Kind() { //nolint:exhaustive // Only containers are walked; every other kind is a leaf.
	case reflect.Pointer:
		w.visit(v.Elem(), key)
	case reflect.Struct:
		w.visitFields(v, key)
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			w.visit(v.Index(i), key+"["+strconv.Itoa(i)+"]")
		}
	case reflect.Map:
		w.visitEntries(v, key)
	}
}

// visitFields visits struct v's exported fields, except those tagged validate:"-".
func (w *walker) visitFields(v reflect.Value, key string) {
	for f, field := range v.Fields() {
		if f.IsExported() && f.Tag.Get("validate") != "-" {
			w.visit(field, joinKey(key, fieldSegment(f)))
		}
	}
}

// visitEntries visits map v's values in the order of their entries' names. Each value is copied first, since a map
// value isn't addressable and a pointer-receiver Validate method couldn't be found on it otherwise.
func (w *walker) visitEntries(v reflect.Value, key string) {
	type entry struct {
		name  string
		value reflect.Value
	}

	entries := make([]entry, 0, v.Len())

	for k, value := range v.Seq2() {
		entries = append(entries, entry{name: fmt.Sprint(k), value: value})
	}

	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Compare(a.name, b.name)
	})

	for _, e := range entries {
		w.visit(addressable(e.value), entryKey(key, e.name))
	}
}

// call runs validator, at key, unless a tag failed at or beneath key, and keeps what it returns.
func (w *walker) call(validator Validator, key string) {
	for _, failed := range w.failed {
		if within(failed, key) {
			return
		}
	}

	err := validator.Validate()
	if err != nil {
		w.violations = append(w.violations, prefix(key, err)...)
	}
}

// asValidator returns v as a Validator, through its value or, when v is addressable, its address.
func asValidator(v reflect.Value) (Validator, bool) {
	if validator, ok := reflect.TypeAssert[Validator](v); ok {
		return validator, true
	}

	if v.CanAddr() {
		validator, ok := reflect.TypeAssert[Validator](v.Addr())

		return validator, ok
	}

	return nil, false
}

// addressable returns an addressable copy of v.
func addressable(v reflect.Value) reflect.Value {
	c := reflect.New(v.Type()).Elem()

	c.Set(v)

	return c
}
