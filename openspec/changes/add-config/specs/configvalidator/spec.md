## Purpose

Checks a configuration struct the same way wherever it's checked, after loading or inside a module's provider, so every violation is reported at once, names the config key an operator would write, and never echoes the value.

## ADDED Requirements

### Requirement: Tag rules apply to every reachable field except skipped ones

Validation SHALL evaluate the `validate` tag rules on every exported field of the config and of every struct reachable from it through struct fields, embedded structs, non-nil pointers and non-nil interface values. A field tagged `validate:"-"` SHALL be neither evaluated nor descended into, so no rule beneath it, `required` included, can fail through that field.

#### Scenario: Nested rule fails
- **WHEN** the config's `http.port` field has the rule `min=1024` and holds 80
- **THEN** validation reports a violation for `http.port`

#### Scenario: Dormant mode struct is skipped
- **WHEN** a messaging config marks its `amqp` field `validate:"-"`, the AMQP config's `url` is `required` and empty, and the messaging mode is `file`
- **THEN** validation reports no violation for `messaging.amqp.url`

#### Scenario: Skipped struct is checked when validated directly
- **WHEN** the same AMQP config, with its `url` empty, is validated on its own
- **THEN** validation reports a violation for `url`

#### Scenario: Nil section is skipped
- **WHEN** a pointer-to-struct field is nil
- **THEN** no rule inside that struct is evaluated

### Requirement: A Validate method owns its branch

When a value reachable from the config has a Validate method, with a value or a pointer receiver, validation SHALL call it once and SHALL NOT look for Validate methods anywhere beneath it; the method decides which of its children to check. Beneath values without one, validation SHALL keep looking through exported struct fields, embedded structs, non-nil pointers, slice and array elements, map values and non-nil interface values, but never through a field tagged `validate:"-"`. A value whose type decodes itself from text SHALL be treated as a single value: its own Validate method is called, and nothing inside it is searched.

#### Scenario: The outermost method wins
- **WHEN** a parent config and its child both have Validate methods and the parent's doesn't delegate to the child
- **THEN** only the parent's method is called

#### Scenario: The walk finds methods below a plain parent
- **WHEN** the app config has no Validate method and its `http` and `cache` sections each have one
- **THEN** both sections' methods are called

#### Scenario: Skipped field hides its Validate method
- **WHEN** a config without a Validate method marks a field `validate:"-"` and that field's type has a Validate method that would fail
- **THEN** the method is not called

#### Scenario: Pointer receiver on the root
- **WHEN** the config's own Validate method has a pointer receiver
- **THEN** it is called

#### Scenario: Elements are walked
- **WHEN** the config has a slice of host configs and a map of database configs, each element with a Validate method
- **THEN** the method is called for every slice element and every map value

### Requirement: A Validate method doesn't run on a branch whose tag rules failed

Validation SHALL NOT call a Validate method when a tag rule failed on that value or anywhere beneath it, since the method may rely on what its tags guarantee. Validate methods in other branches SHALL still be called.

#### Scenario: Shape failure suppresses the semantic check
- **WHEN** `messaging.mode` fails its `oneof` rule and the `http` section's Validate method returns an error
- **THEN** the messaging config's Validate method is not called, and validation reports both the `messaging.mode` violation and the `http` failure

### Requirement: Every violation is reported

Validation SHALL report every violation it finds, not only the first: every failing tag rule, and the failure of every Validate method it calls. It SHALL return no error when it finds none. Each violation SHALL be retrievable on its own as an error carrying its key, and all of them SHALL be retrievable together as one list, even after the caller wraps the result with its own context. An error returned by a Validate method SHALL stay matchable with errors.Is through the result.

#### Scenario: Violations in two sections
- **WHEN** `http.port` and `cache.ttl` both fail their tag rules
- **THEN** validation reports both, each with its own key

#### Scenario: Violations survive the caller's wrapping
- **WHEN** a caller wraps a result holding violations for `http.port` and `cache.ttl` with its own context
- **THEN** both violations are still retrievable together, each with its key

#### Scenario: Sentinel stays reachable
- **WHEN** a Validate method returns an error wrapping a module's exported sentinel
- **THEN** errors.Is matches that sentinel against the validation result

#### Scenario: Valid config
- **WHEN** no rule fails and no Validate method returns an error
- **THEN** validation returns no error

### Requirement: Violations name the config key

Each violation SHALL carry the dotted key an operator would write for the failing value: each field's mapstructure name, or its Go field name when it has none. A squashed embedded struct SHALL add no segment, for tag rules and Validate methods alike. A slice or array element SHALL appear as the key followed by its index in brackets, and a map value as the key, a dot and the entry's key. A failure of the config's own Validate method, or of a rule on the config as a whole, SHALL have an empty key. A violation SHALL read as the key, a colon, a space and the reason, or just the reason when the key is empty, with no prefix of its own; callers add their own context.

#### Scenario: Untagged field
- **WHEN** a failing field has no mapstructure tag
- **THEN** its segment of the key is the Go field name

#### Scenario: Squashed embed is flattened
- **WHEN** a tag rule fails on `host` inside a struct squashed into the `broadcast.http` section
- **THEN** the key is `broadcast.http.host`

#### Scenario: Slice element
- **WHEN** the third element of `hosts` fails
- **THEN** the key is `hosts[2]`

#### Scenario: Map value
- **WHEN** the `reporting` entry of the `databases` map fails
- **THEN** the key is `databases.reporting`

#### Scenario: Root failure
- **WHEN** the config's own Validate method returns "ports overlap"
- **THEN** the violation reads "ports overlap"

### Requirement: Delegated violations keep their full key

When a Validate method validates one of its fields with ValidateField, given its receiver and that field, and returns the result, each violation SHALL be reported under the method's key, the field's key and the violation's own key joined, with no context repeated. This SHALL hold for value and pointer receivers alike, and for a field tagged `validate:"-"`. The same SHALL hold when a Validate method returns keyed errors it built itself, and when it returns several violations joined together. ValidateField SHALL panic when the field isn't beneath the receiver, since that's a bug in the method rather than in the config.

#### Scenario: Mode dispatch
- **WHEN** the `messaging` section's Validate method validates its `amqp` field with ValidateField, and the AMQP config's `url` fails
- **THEN** the violation's key is `messaging.amqp.url` and its text names that key once

#### Scenario: Pointer receiver
- **WHEN** the same Validate method has a pointer receiver
- **THEN** the violation's key is still `messaging.amqp.url`

#### Scenario: Method-built keyed error
- **WHEN** the `http.ssl` section's Validate method returns a keyed error for `cert_path`
- **THEN** the violation's key is `http.ssl.cert_path`

#### Scenario: Several delegated violations
- **WHEN** a delegated child reports violations for `url` and `queue`
- **THEN** both are reported, each under the parent's key

#### Scenario: Field outside its parent
- **WHEN** a Validate method passes ValidateField a field of a different struct
- **THEN** ValidateField panics

### Requirement: Tag violations never contain the value

A violation from a tag rule SHALL name the rule and its parameter, if it has one, and SHALL NOT contain the field's value, since configuration holds secrets. Errors returned by Validate methods SHALL be reported as they are.

#### Scenario: Secret fails a rule
- **WHEN** a `password` field holding "hunter2" fails the rule `min=12`
- **THEN** the violation names `min=12` and doesn't contain "hunter2"

#### Scenario: Rule parameter is shown
- **WHEN** `mode` fails the rule `oneof=read write`
- **THEN** the violation names `oneof=read write`

### Requirement: A nil config is an error

Validation SHALL return an error, and SHALL NOT panic, when it's given a nil config.

#### Scenario: Nil pointer
- **WHEN** validation is called with a nil pointer
- **THEN** it returns an error
