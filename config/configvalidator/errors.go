package configvalidator

import (
	"errors"
	"reflect"
	"strings"
)

// ErrNilConfig is returned by Validate when it's given a nil config, and by ValidateField when the field is nil.
var ErrNilConfig = errors.New("config is nil")

// joinType is the type errors.Join returns.
var joinType = reflect.TypeOf(errors.Join(ErrNilConfig))

// Errors is every violation Validate found, each an *Error, in the order it found them: failed tags first, in the
// order of their keys, then what Validators returned. Validate returns one only when it holds at least one violation.
// errors.AsType gets it back through any wrapping, and errors.Is and errors.AsType look inside each violation.
type Errors []error

// Error returns each violation's message on its own line, skipping nil entries.
func (e Errors) Error() string {
	return strings.Join(messages(e), "\n")
}

// Unwrap returns the violations, so errors.Is and errors.AsType check each of them.
func (e Errors) Unwrap() []error {
	return e
}

// orNil returns e, or nil when e is empty, so an error result is never a non-nil empty Errors.
func (e Errors) orNil() error {
	if len(e) == 0 {
		return nil
	}

	return e
}

// Error is one violation found by Validate: the config key it's about and what's wrong there. Validate returns each
// violation as an *Error inside an Errors, and a Validator can return one to name a child's key.
type Error struct {
	// Key is the dotted config key of the failing value, as an operator would write it: "http.port", "hosts[2]" or
	// "databases.reporting". It's empty when the failure is about the config as a whole.
	Key string

	// Err is what's wrong: a *RuleError for a failed validate tag, or what a Validator returned.
	Err error

	// at is Key as a path, kept on the violations Validate builds so they join a parent's key without Key being
	// parsed. It's ignored once Key no longer matches it.
	at path
}

// newError returns a violation of err at at.
func newError(at path, err error) *Error {
	return &Error{Key: at.String(), Err: err, at: at}
}

// Error returns "key: cause", or the cause alone when Key is empty. A nil Err reads as "invalid".
func (e *Error) Error() string {
	cause := "invalid"

	if e.Err != nil {
		cause = e.Err.Error()
	}

	if e.Key == "" {
		return cause
	}

	return e.Key + ": " + cause
}

// Unwrap returns Err, so errors.Is and errors.AsType see through the key to the cause.
func (e *Error) Unwrap() error {
	return e.Err
}

// path returns e's key as a path: the one it was built with while Key still matches it, and otherwise Key as written.
func (e *Error) path() path {
	switch {
	case e.at != nil && e.Key == e.at.String():
		return e.at
	case e.Key == "":
		return nil
	default:
		return path{{kind: rawKind, name: e.Key}}
	}
}

// RuleError is the cause of a violation found by a validate tag. It names the rule and never the value, since config
// values include secrets.
type RuleError struct {
	// Tag is the rule that failed, such as "min" or "oneof".
	Tag string

	// Param is the rule's parameter, such as "1024" or "read write", or empty when it has none.
	Param string
}

// Error returns "fails tag=param", or "fails tag" when Param is empty.
func (e *RuleError) Error() string {
	if e.Param == "" {
		return "fails " + e.Tag
	}

	return "fails " + e.Tag + "=" + e.Param
}

// prefix returns the violations in err, each keyed beneath at. A top-level *Error becomes a new *Error with at joined
// to its own key, and a nil one is no violation. An Errors, or what errors.Join returns, is split and each part handled
// the same way. Anything else, nil aside, becomes an *Error at at. err is never modified.
//
// A keyed error wrapped by fmt.Errorf is in the last group, even when the wrapper joins several errors with %w: its
// text is kept, and any key inside it is part of that text rather than the result's Key.
func prefix(at path, err error) Errors {
	if violation, ok := err.(*Error); ok { //nolint:errorlint // Only a top-level *Error composes; one wrapped is opaque.
		if violation == nil {
			return nil
		}

		return Errors{newError(at.join(violation.path()), violation.Err)}
	}

	if parts, ok := split(err); ok {
		var violations Errors

		for _, part := range parts {
			violations = append(violations, prefix(at, part)...)
		}

		return violations
	}

	if err == nil {
		return nil
	}

	return Errors{newError(at, err)}
}

// split returns the errors err joins, and false when err isn't a join to split: an Errors, even an empty one, or what
// errors.Join returns. Any other error stays whole, even one with an Unwrap() []error method, such as a fmt.Errorf
// wrapper with several %w verbs, whose text says more than its parts.
func split(err error) ([]error, bool) {
	if errs, ok := err.(Errors); ok { //nolint:errorlint // Only err itself splits; a join it wraps is part of its text.
		return errs, true
	}

	if reflect.TypeOf(err) == joinType {
		joined, _ := err.(interface{ Unwrap() []error })

		return joined.Unwrap(), true
	}

	return nil, false
}

// messages returns the message of each error in errs, skipping nil ones, typed-nil *Error values included.
func messages(errs []error) []string {
	result := make([]string, 0, len(errs))

	for _, err := range errs {
		if violation, ok := err.(*Error); err == nil || ok && violation == nil { //nolint:errorlint // See prefix.
			continue
		}

		result = append(result, err.Error())
	}

	return result
}
