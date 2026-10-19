package configvalidator

import "reflect"

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

// holdsValidator reports whether a value of type t could be, or hold, a Validator.
func holdsValidator(t reflect.Type) bool {
	return containerKind(t.Kind()) || implementsValidator(t)
}

// implementsValidator reports whether t, or a pointer to t, has a Validate method.
func implementsValidator(t reflect.Type) bool {
	return t.Implements(validatorType) || reflect.PointerTo(t).Implements(validatorType)
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
