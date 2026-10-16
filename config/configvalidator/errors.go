package configvalidator

import (
	"errors"
	"strings"
)

// ErrNilConfig is returned by Validate when it's given a nil config.
var ErrNilConfig = errors.New("config is nil")

// Errors is every violation Validate found, each an *Error, in the order it found them: failed tags first, then what
// Validators returned. Validate returns one only when it holds at least one violation. errors.AsType gets it back
// through any wrapping, and errors.Is and errors.AsType look inside each violation.
type Errors []error

// Error returns each violation's message on its own line.
func (e Errors) Error() string {
	var b strings.Builder

	for i, err := range e {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(err.Error())
	}

	return b.String()
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
// joined to its own, an Errors or other joined error is split and each part handled the same way, and anything else
// becomes an *Error at key. err is never modified.
//
// A keyed error wrapped by fmt.Errorf is in the last group: its text is kept, and its own key is part of that text
// rather than the result's Key.
func prefix(key string, err error) Errors {
	//nolint:errorlint // Only a top-level *Error or join composes; anything wrapping one is opaque by design.
	switch err := err.(type) {
	case nil:
		return nil
	case *Error:
		if key == "" {
			return Errors{err}
		}

		return Errors{&Error{Key: joinKey(key, err.Key), Err: err.Err}}
	case interface{ Unwrap() []error }:
		var violations Errors

		for _, part := range err.Unwrap() {
			violations = append(violations, prefix(key, part)...)
		}

		return violations
	default:
		return Errors{&Error{Key: key, Err: err}}
	}
}
