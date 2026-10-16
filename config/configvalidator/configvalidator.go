package configvalidator

import (
	"fmt"
	"iter"
	"reflect"
)

var validatorType = reflect.TypeFor[Validator]()

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
// It runs in two passes. First the validate tags on every field reachable through struct fields and non-nil pointers
// are evaluated, except beneath a field tagged validate:"-"; when cfg is a slice, array or map, each element is
// checked that way, under its index or entry. Then cfg is walked through exported and embedded struct fields,
// pointers, slice and array elements, map values and interface values, again skipping validate:"-" fields, and
// Validate is called on each outermost Validator found, through its value or its address. A Validator isn't called
// when a tag failed on it or beneath it, since its checks may rely on what its tags guarantee, nor when its Validate
// method may be promoted from an embedded pointer that's nil: that section is missing, and the walk carries on into
// the struct's fields instead.
//
// A type that decodes itself from text (see DecodesText) is a single value: its own Validate method is called, and
// nothing inside it is searched. A map entry is keyed by its key's text form or underlying value. Tag violations name
// the rule and its parameter, never the value. What a Validator returns is kept as it is, under its key.
//
// Validate returns ErrNilConfig when cfg is nil. It's safe for concurrent use.
func Validate[T any](cfg *T) error {
	if cfg == nil {
		return ErrNilConfig
	}

	return validate(reflect.ValueOf(cfg).Elem()).orNil()
}

// ValidateField validates field, which must point to a value beneath the one parent points to, and reports each
// violation under field's key within parent. It's how a Validate method checks one of its fields: for a messaging
// config whose amqp field fails on url, ValidateField(m, &m.AMQP) reports "amqp.url", and the walk adds the method's
// own key to make "messaging.amqp.url". With Validate(&m.AMQP) instead, the "amqp" segment would be lost.
//
// field is found by its address, through exported and embedded struct fields, non-nil pointers and interfaces, slice
// and array elements, and map values, including beneath fields tagged validate:"-". Parent and field come from the
// same receiver, so this works with value and pointer receivers alike.
//
// A nil field returns ErrNilConfig, under the field's key when parent holds exactly one nil pointer of field's type,
// as it does when an optional section is missing. ValidateField panics when parent is nil or field isn't beneath it,
// since that's a bug in the calling method rather than a problem with the config.
func ValidateField[P, F any](parent *P, field *F) error {
	root := reflect.ValueOf(parent).Elem()

	if field == nil {
		if at, ok := findNil(root, reflect.TypeFor[*F]()); ok {
			return Errors{newError(at, ErrNilConfig)}
		}

		return ErrNilConfig
	}

	target := reflect.ValueOf(field)

	at, ok := findField(root, nil, target)

	if !ok {
		panic(fmt.Sprintf("configvalidator: ValidateField: %T isn't a field beneath %T", field, parent))
	}

	return prefix(at, validate(target.Elem())).orNil()
}

// validate runs both passes over root and returns every violation, keyed beneath root.
func validate(root reflect.Value) Errors {
	var w walker

	for _, violation := range checkTags(root) {
		w.failed = append(w.failed, violation.at)
		w.violations = append(w.violations, violation)
	}

	w.visit(root, nil)

	return w.violations
}

// findField returns the path of the value target points to, which must sit beneath v, whose path is at. v itself is
// never a match, so a parent can't be its own field.
func findField(v reflect.Value, at path, target reflect.Value) (path, bool) {
	for childAt, child := range children(v, at, target.Type().Elem()) {
		if isAt(child, target) {
			return childAt, true
		}

		if found, ok := findField(child, childAt, target); ok {
			return found, true
		}
	}

	return nil, false
}

// findNil returns the path of the one nil pointer of type t beneath v, and false when there's none or more than one.
func findNil(v reflect.Value, t reflect.Type) (path, bool) {
	var (
		found path
		count int
		find  func(reflect.Value, path)
	)

	find = func(v reflect.Value, at path) {
		for childAt, child := range children(v, at, t) {
			if child.Type() == t && child.IsNil() {
				found = childAt
				count++
			}

			find(child, childAt)
		}
	}

	find(v, nil)

	return found, count == 1
}

// children yields the values directly beneath v whose addresses can be taken through v, with their paths: what a
// non-nil pointer or interface holds, a struct's exported and embedded fields, and the elements of a slice, array or
// map that could be or hold a value of type want.
func children(v reflect.Value, at path, want reflect.Type) iter.Seq2[path, reflect.Value] {
	switch v.Kind() { //nolint:exhaustive // Only these kinds hold values beneath them.
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return pointee(v, at)
		}
	case reflect.Struct:
		return fields(v, at)
	case reflect.Slice, reflect.Array:
		if holds(v.Type().Elem(), want) {
			return elements(v, at)
		}
	case reflect.Map:
		if holds(v.Type().Elem(), want) {
			return values(v, at)
		}
	}

	return func(func(path, reflect.Value) bool) {}
}

// pointee yields what pointer or interface v holds, at v's own path.
func pointee(v reflect.Value, at path) iter.Seq2[path, reflect.Value] {
	return func(yield func(path, reflect.Value) bool) {
		yield(at, v.Elem())
	}
}

// fields yields struct v's exported and embedded fields.
func fields(v reflect.Value, at path) iter.Seq2[path, reflect.Value] {
	return func(yield func(path, reflect.Value) bool) {
		for f, field := range v.Fields() {
			if reachable(f) && !yield(at.child(fieldSegment(f)), field) {
				return
			}
		}
	}
}

// elements yields slice or array v's elements.
func elements(v reflect.Value, at path) iter.Seq2[path, reflect.Value] {
	return func(yield func(path, reflect.Value) bool) {
		for i := range v.Len() {
			if !yield(at.child(segment{kind: indexKind, index: i}), v.Index(i)) {
				return
			}
		}
	}
}

// values yields map v's values, which aren't addressable themselves but may hold pointers to values that are.
func values(v reflect.Value, at path) iter.Seq2[path, reflect.Value] {
	return func(yield func(path, reflect.Value) bool) {
		for k, value := range v.Seq2() {
			if !yield(at.child(segment{kind: entryKind, name: entryName(k)}), value) {
				return
			}
		}
	}
}

// holds reports whether a value of type t could be, or hold, a value of type want: it is want, or a kind that holds
// other values. A slice of bytes or strings is then passed over without looking at each element.
func holds(t, want reflect.Type) bool {
	return t == want || containerKind(t.Kind())
}

// holdsValidator reports whether a value of type t could be, or hold, a Validator.
func holdsValidator(t reflect.Type) bool {
	return containerKind(t.Kind()) || implementsValidator(t)
}

// containerKind reports whether a value of kind k can hold other values.
func containerKind(k reflect.Kind) bool {
	switch k { //nolint:exhaustive // Every other kind is a single value.
	case reflect.Interface, reflect.Pointer, reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}

// implementsValidator reports whether t, or a pointer to t, has a Validate method.
func implementsValidator(t reflect.Type) bool {
	return t.Implements(validatorType) || reflect.PointerTo(t).Implements(validatorType)
}

// isAt reports whether v is the value target points to.
func isAt(v, target reflect.Value) bool {
	return v.CanAddr() && v.Type() == target.Type().Elem() && v.Addr().UnsafePointer() == target.UnsafePointer()
}

// walker finds the Validators in a config and collects what they return.
type walker struct {
	// failed holds the path of every tag violation.
	failed []path

	violations Errors
}

// visit calls the outermost Validators at or beneath v, whose path is at.
func (w *walker) visit(v reflect.Value, at path) {
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return
	}

	if v.Kind() == reflect.Interface {
		w.visit(addressable(v.Elem()), at)

		return
	}

	if validator, ok := asValidator(v); ok && !embedsNilValidator(v) {
		w.call(validator, at)

		return
	}

	if DecodesText(v.Type()) {
		return
	}

	switch v.Kind() { //nolint:exhaustive // Only containers are walked; every other kind is a leaf.
	case reflect.Pointer:
		w.visit(v.Elem(), at)
	case reflect.Struct:
		w.visitFields(v, at)
	case reflect.Slice, reflect.Array:
		if !holdsValidator(v.Type().Elem()) {
			return
		}

		for i := range v.Len() {
			w.visit(v.Index(i), at.child(segment{kind: indexKind, index: i}))
		}
	case reflect.Map:
		if !holdsValidator(v.Type().Elem()) {
			return
		}

		w.visitEntries(v, at)
	}
}

// visitFields visits struct v's exported and embedded fields, except those tagged validate:"-".
func (w *walker) visitFields(v reflect.Value, at path) {
	for f, field := range v.Fields() {
		if reachable(f) && f.Tag.Get("validate") != "-" {
			w.visit(field, at.child(fieldSegment(f)))
		}
	}
}

// visitEntries visits map v's values in the order of their entries' names. Each value is copied first, since a map
// value isn't addressable and a pointer-receiver Validate method couldn't be found on it otherwise.
func (w *walker) visitEntries(v reflect.Value, at path) {
	for _, e := range sortedEntries(v) {
		w.visit(addressable(e.value), at.child(segment{kind: entryKind, name: e.name}))
	}
}

// call runs validator at at, unless a tag failed at or beneath at, and keeps what it returns.
func (w *walker) call(validator Validator, at path) {
	for _, failed := range w.failed {
		if failed.within(at) {
			return
		}
	}

	w.violations = append(w.violations, prefix(at, validator.Validate())...)
}

// embedsNilValidator reports whether v, a struct or a pointer to one, embeds a nil pointer or interface whose type has
// a Validate method, directly or through other embedded structs. A Validate method v has may then be promoted from
// that missing section and have no receiver to run on, so v isn't treated as a Validator. Reflection can't tell a
// promoted method from one v declares, so a type that declares its own Validate is treated the same way.
func embedsNilValidator(v reflect.Value) bool {
	v, ok := indirect(v)

	if !ok || v.Kind() != reflect.Struct {
		return false
	}

	for f, field := range v.Fields() {
		if !f.Anonymous {
			continue
		}

		if (field.Kind() == reflect.Pointer || field.Kind() == reflect.Interface) && field.IsNil() {
			if implementsValidator(f.Type) {
				return true
			}

			continue
		}

		if embedsNilValidator(field) {
			return true
		}
	}

	return false
}

// asValidator returns v as a Validator, through its value or, when v is addressable, its address. A value reached
// through an unexported field can't be used this way, and isn't one.
func asValidator(v reflect.Value) (Validator, bool) {
	if !v.CanInterface() {
		return nil, false
	}

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
