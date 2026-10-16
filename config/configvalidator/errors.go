package configvalidator

import (
	"errors"
)

// ErrNilConfig is returned by Validate when it's given a nil config.
var ErrNilConfig = errors.New("config is nil")

// Error is one violation found by Validate: the config key it's about and what's wrong there. Validate returns each
// violation as an *Error, alone or joined with the others, and a Validator can return one to name a child's key.
type Error struct {
	// Key is the dotted config key of the failing value, as an operator would write it: "http.port", "hosts[2]" or
	// "databases.reporting". It's empty when the failure is about the config as a whole.
	Key string

	// Err is what's wrong: a *RuleError for a failed validate tag, or what a Validator returned.
	Err error
}

// Error returns "key: cause", or the cause alone when Key is empty.
func (e *Error) Error() string {
	if e.Key == "" {
		return e.Err.Error()
	}

	return e.Key + ": " + e.Err.Error()
}

// Unwrap returns Err, so errors.Is and errors.AsType see through the key to the cause.
func (e *Error) Unwrap() error {
	return e.Err
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

// prefix returns the violations in err, each keyed beneath key. A top-level *Error becomes a new *Error with key
// joined to its own, a joined error is split and each part handled the same way, and anything else becomes an
// *Error at key. err is never modified.
//
// A keyed error wrapped by fmt.Errorf is in the last group: its text is kept, and its own key is part of that text
// rather than the result's Key.
func prefix(key string, err error) []error {
	//nolint:errorlint // Only a top-level *Error or join composes; anything wrapping one is opaque by design.
	switch err := err.(type) {
	case nil:
		return nil
	case *Error:
		if key == "" {
			return []error{err}
		}

		return []error{&Error{Key: joinKey(key, err.Key), Err: err.Err}}
	case interface{ Unwrap() []error }:
		var violations []error

		for _, part := range err.Unwrap() {
			violations = append(violations, prefix(key, part)...)
		}

		return violations
	default:
		return []error{&Error{Key: key, Err: err}}
	}
}
