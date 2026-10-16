## Context

The source is `configloader` in idio/go-modules (0.3.2 there): `Load`, `Validate`, an env-binding walk and fx glue in one module over viper and go-playground/validator. See proposal.md for why it's split and rebuilt rather than copied; the requirements are in specs/configvalidator/spec.md and specs/configloader/spec.md. This document covers how they're met and what was rejected.

Facts that shape the approach, checked against the libraries' sources:

- **Viper only unmarshals keys it knows.** `Unmarshal` visits `AllKeys()`, built from overrides, bound env names, the config file and defaults. `AutomaticEnv` makes `Get` check the environment for any key, but `Unmarshal` never asks about a key outside `AllKeys()`. With `AutomaticEnv` on, an env variable named after a section (`MY_SVC_HTTP`) shadows every key beneath it, and viper's derived name is checked before explicitly bound names.
- **`BindEnv(key, name)` uses `name` verbatim.** No prefix or key replacer is applied unless one is configured.
- **Viper lowercases keys,** including map keys from files and from `SetDefault`.
- **Viper's struct binding** (`decodeStructKeys`) sits behind an internal feature flag, so a reflection walk is still needed.
- **go-playground's `validate:"-"` drops a field from the type cache,** so nothing behind it is visited. `StructNamespace()` reports Go field names whatever tag-name function is registered. Struct values inside slices and maps are checked only when the field carries `dive`.
- **mapstructure v2.4.0 and later keep input values out of its error messages** (the fixes for GHSA-fv92-fjc5-jj9h and GHSA-2464-8j7c-4cjm). Its `DecodeError` names the dotted field path. `TextUnmarshallerHookFunc` returns `UnmarshalText` errors unchanged, and those often echo the input: `netip.ParsePrefix("…")`, `slog: level string "…"`. `StringToWeakSliceHookFunc` exists; viper's own copy is unexported.
- **mapstructure doesn't skip a field tagged `mapstructure:"-"` when decoding** (checked against v2.5.0): it reads `-` as the field's key. The key rules still treat such a field as having no key. No file sensibly has a `-` key, and the loader's unknown-key check rejects one.
- **Constitution constraints:** `WithX` names are reserved for `fx.Supply` wrappers, Params structs embed `fx.In`, documented failure modes are exported sentinels, and modules don't log through package-level slog.

## Goals / Non-Goals

**Goals:**

- One set of key rules (mapstructure names, squash, skipped fields, text-decoded values) behind error keys, env names, the unknown-key check and defaults, so they can't drift apart.
- No viper type in either module's API.
- Every spec scenario testable without a network, a fixed path outside the test's temp directory, or global state beyond `t.Setenv` and `t.Chdir`.

**Non-Goals:**

- **Changing how go-playground evaluates tags.** Element rules still need `dive`. The walk adds Validate calls inside slices and maps, not tag checks.
- **Restricting the search to some formats.** The search tries every extension viper can decode. Strict unknown keys catch most wrong-file pickups.
- **Viper versions without `BindEnv(key, name)` or `NewCodecRegistry`.** Both exist in the viper versions we'd require.

## Decisions

### 1. The key rules live in config/configvalidator, and the loader imports them (→ docs/config.md)

The validator needs keys for its errors; the loader needs the same keys for env names, defaults and the unknown-key check. `config/configvalidator` exports two small helpers:
- one that maps a struct field to its key segment: its mapstructure name, the Go name when untagged, no segment when squashed, excluded when tagged `mapstructure:"-"` or unexported;
- one that reports whether a type decodes itself from text: it, or a pointer to it, implements `encoding.TextUnmarshaler`.

The loader imports both. The loader already depends on `config/configvalidator`, so this adds no edge, and env names can't drift from the keys in validation errors.

Alternatives:
- **Duplicating the rules in the loader,** about 40 lines with a shared test matrix. It keeps the validator's API narrower, but the two copies drift the first time one gains a case (`,remain`, say) and the other doesn't.
- **A third module for the key rules.** It's a module and a release for two functions.

### 2. Tag pass: go-playground, with keys translated from Go-name namespaces

Under the new layout rule (decision 14), the directories are `config/configvalidator` and `config/configloader`, and the packages share those names, like `net/http/httptest`. Directory, package and import name all agree, so there are no aliases and no mismatch:
- **No generic identifiers in app code.** A bare `validator` or `loader` could collide with an app's own packages.
- **No clash inside the package.** go-playground's package, named `validator`, imports without an alias.
- **No stutter.** The interface reads `configvalidator.Validator`.

The import paths repeat their group (`config/configloader`), which is the price of keeping the `config/` grouping. Alternatives:
- **`config/loader` with a `configloader` package clause.** Same identifiers, but it breaks "package name = last path element".
- **`config/loader` with an import alias in docs.** Consumers who skip the docs get `loader`.
- **Flat `configloader/` directories.** Idiomatic, but loses the grouping.

One package-level go-playground `Validate` instance, built with `WithRequiredStructEnabled`. It caches per type and is safe for concurrent use. No tag-name function is registered. Each `FieldError`'s `StructNamespace()` (Go names, with `[i]` and `[key]` for dived elements) is translated into a key by following those names through the root value with decision 1's rules. Following the value rather than the type sees through interface fields, which go-playground descends into, and matches a map entry's name against the map's real keys, so a name containing `]` or `.` is still found. The root type's own segment is dropped: it's the type's name, which for a generic type contains dots, and it's absent for an anonymous struct.

This fixes the original's documented caveat: a tag failure inside a squashed embed used to report the embedded type's name as a segment. An anonymous embed without `,squash` keeps a segment, because mapstructure decodes it under its type name.

A tag violation's cause is an exported `*RuleError` carrying the tag and its parameter. It renders as `fails min=1024`, or `fails required` when there's no parameter, and never includes the value. A module's tests can then assert which rule failed with `errors.AsType`.

Alternatives:
- **Translating go-playground's messages, or using its universal-translator.** Its English messages name the Go field and sometimes the value.
- **A tag-name function returning mapstructure names,** as the original did. It can't express "no segment" for squash, because an empty name falls back to the Go name, and `-` would skip the field entirely.

### 3. The walk: outermost Validator, skipped fields, text values, every container

The walk starts from the `*T` it's given, so the root is addressable. At each value:
1. A nil pointer stops the walk.
2. A field tagged `validate:"-"` is never entered (decision 1's tag lookup, applied to the `validate` tag).
3. A text-decoded value is a single value: if it's a Validator it's called, and either way the walk stops there.
4. A Validator, through either the value or its address, is called, unless a tag violation's path lies at or beneath its own. Either way the walk stops there. A struct whose Validate method may be promoted from a nil embedded pointer or interface isn't a Validator: that section is missing, and the walk carries on into its fields.
5. Otherwise the walk descends into exported and embedded struct fields (an unexported embedded struct too, since its exported fields are promoted), slice and array elements (`key[i]`), map values (`key.<entry>`) and non-nil interface values (at the same key), as go-playground's tag pass does. A slice, array or map whose element type can't be or hold a Validator, such as `[]byte`, is passed over without visiting each element.

**Paths, not strings.** Positions are kept as lists of segments, one each for a field, a squashed field, an index or an entry, until a violation's `Key` is written. Suppression compares segments, so an entry named `acme.eu` never counts as beneath `acme`. A squashed field has a segment that writes nothing, so its branch stays apart from its siblings'. An entry is written after a dot whatever it holds, and named by its key's text form or underlying value, not by a `String` method. An `*Error` built by the validator carries its path unexported, so it joins a parent's key without `Key` being parsed. A user-built `*Error` has only `Key`, which joins as written, after a dot unless it starts with `[`.

Map values and interface contents aren't addressable, so each is copied into a new addressable value before the Validator check, which lets pointer-receiver methods be found.

Skipping a Validator whose branch failed its tags (rule 4) keeps "report everything" from calling methods on data their own tags rejected, such as an unknown mode.

Alternatives:
- **Fail-fast like the original.** It's simpler, but every violation costs a redeploy.
- **Run every Validator regardless of tag failures.** It produces duplicate and misleading reports.

### 4. Reporting: Error, errors.Join, and composition through direct returns

Each violation is an `*Error{Key, Err}` with pointer receivers. `Error()` returns `key: <cause>`, or just the cause when the key is empty, and `Unwrap` returns `Err`. Validate returns nil or an `Errors`, a named `[]error` holding one `*Error` per violation, even when there's only one. `Errors` has value receivers, as `go/scanner.ErrorList` does, so callers range over it; its `Error()` puts each violation on its own line, and its `Unwrap() []error` lets `errors.Is` and `errors.AsType` check each one. It's never returned empty. A nil config returns `ErrNilConfig`.

When the walk wraps what a Validator returned, at key K:
- an `*Error` at the top becomes a new `*Error` with key `K.<its key>`; the original is never mutated;
- an `Errors`, or the type `errors.Join` returns, is split, and each part is wrapped the same way;
- a nil `*Error` is no violation;
- anything else becomes `*Error{Key: K, Err: err}`, including an error with an `Unwrap() []error` method of another type, such as `fmt.Errorf` with several `%w` verbs, whose text says more than its parts.

A keyed error buried under `fmt.Errorf("context: %w", …)` is opaque: the context text survives, and the key stops at K. The original composed through any wrapping with `errors.As`, which silently dropped the wrapper's text.

**Delegating to a field.** K is the Validator's own key, so composition alone can't name the field it delegates to: `Validate(&m.AMQP)` returns `url`, and the walk makes `messaging.url`. The original had the same gap. A second export closes it:

```go
func ValidateField[P, F any](parent *P, field *F) error
```

It finds `field` beneath `parent` by address, through struct fields, non-nil pointers and slice and array elements, ignoring validate tags, and builds its key with decision 1's rules. It returns `Validate(field)` with each violation prefixed by that key, so `ValidateField(m, &m.AMQP)` returns `amqp.url` and the walk makes `messaging.amqp.url`. Both pointers come from the same receiver, so it works with value receivers too: `&m` and `&m.AMQP` both point into the copy. It also finds a field behind map values, as `ValidateField(&d, d.DBs[d.Active])` passes for a map of pointers. A nil field returns `ErrNilConfig`, keyed at the field when the parent holds exactly one nil pointer of that type, so a missing optional section reads `messaging.amqp: config is nil`. A nil parent, or a field that isn't beneath the parent, the parent itself included, panics: it's a bug in the method, not a config problem, and the first test of that mode finds it.

The rule for Validator authors, in godoc and docs: return `configvalidator.ValidateField(c, &c.Field)`, or join several results.

Alternatives:
- **Pointer identity behind an unchanged `Validate(&c.Child)`.** `Validate` would tag its errors with the child's address and the walk would look it up beneath the Validator. It only works for pointer receivers; with a value receiver the address points into a copy, and the segment is silently dropped.
- **The author names the key,** as in `&Error{Key: "amqp", Err: Validate(&m.AMQP)}`. It needs no new export, but repeats the mapstructure tag as a string that can drift from it.
- **Delegated keys stop at the Validator,** as in the original. Operators get `messaging.url`, a key they can't write.
- **`errors.Join`, with a single violation returned as a bare `*Error`.** It's standard, but callers handle two shapes and get the list back only through an anonymous `Unwrap() []error` assertion. `Errors` is one shape, found with `errors.AsType[configvalidator.Errors]` through any wrapping, and it renders like a join.
- **A one-line rendering.** Configs with many violations would make one unreadable line; multi-line output prints fine through cobra.
- **Keeping the original's built-in "validating configuration" prefix.** Callers wrap with their own context per the constitution: the loader with `validating config: %w`, a module's provider with its own gerund.

### 5. Loader API: a plain generic Options struct, not functional options

```go
type Options[T any] struct {
    Name             string   // required; sets search directories and the env prefix
    Filename         string   // explicit config file; empty means search
    SearchPaths      []string // searched for config.<ext> before any other place
    Defaults         T        // typed defaults; the zero value means none
    AllowUnknownKeys bool
    Logger           *slog.Logger // where loading is reported; nil discards (decision 17)
}

func Load[T any](o Options[T]) (T, error)
func Provide[T any](o Options[T]) fx.Option
```

T is inferred from the literal: `configloader.Load(configloader.Options[Config]{Name: "my-svc"})`. A plain struct matches the repo's style (constructors take a plain Params struct), the zero value of every field means "not set", and it sidesteps two problems functional options have here:
- **The naming clash.** The constitution reserves `WithX` for `fx.Supply` wrappers, so `WithDefaults` and `WithSearchPath` would read as fx helpers next to the real one, `WithFilename`.
- **Typed defaults need a generic option type.** Then every option that doesn't mention T needs explicit instantiation (`configloader.SearchPath[Config](dir)`).

Alternatives:
- **The original `Load[T](name, filename, opts ...Option)`.** It has the clash, the empty-string filename sentinel, and `func(*viper.Viper)` defaults.
- **`Option[T]` functional options.** Type-safe but noisy at call sites.
- **Non-generic options with `Defaults(any)` checked at run time.** That trades a compile error for a load error.

### 6. The load pipeline, and why it runs in this order

```
1. name ──▶ validate, derive prefix
2. file ──▶ explicit or search ──▶ ReadInConfig ──▶ fileKeys = AllKeys()   (config only, so far)
3. table ─▶ walk T; map fields get entries from fileKeys ∪ entries in Defaults
4. checks ▶ env-name collisions; unknown file keys (unless allowed)
5. layers ▶ SetDefault per leaf of Defaults; per table entry: X_FILE → Set, else BindEnv(key, X)
6. decode ▶ UnmarshalExact (or Unmarshal) into a zero T, with decision 11's hooks
7. validate ▶ config/configvalidator, wrapped "validating config: %w"
```

- **The file comes before the table** because map entries come from it.
- **The table comes before the layers** so that `AllKeys()` in step 2 holds only the file's keys, and so that collisions and unknown keys fail before anything is read from the environment.
- **Any failure returns the zero T.**
- **A fresh `viper.New()` per load,** and nothing touches viper's package-level instance or the process environment.

### 7. Finding the file

- **Explicit path:** `SetConfigFile(path)`. Viper picks the format from the extension. A missing, unreadable, malformed or unsupported file is wrapped as `reading config file "<path>": %w`, so `errors.Is(err, fs.ErrNotExist)` still works.
- **Search: our own loop over candidates, most specific first,** then `SetConfigFile` on the first file found, exactly as for an explicit path:
  1. `config.<ext>` in each of `SearchPaths`;
  2. `.<name>.<ext>` in the working directory;
  3. `config.<ext>` in `<home>/.config/<name>`, `<home>/.<name>` and `/etc/<name>`.

  Viper's own search looks for one file name across directories, so it can't look for `.<name>` in one place and `config` in the others. Each candidate is tried with every extension viper both supports and can decode, in viper's order, so a stray `config.ini` (no decoder since viper 1.21) never stops the search.
  - **The working directory's file is named after the app,** so a project's unrelated `config.yaml` is never read. That also means it no longer has to come last: a project-local file overrides the user's, as `.golangci.yml` does.
  - **The user's directories come before `/etc`,** since the first file found wins and a system-wide file shouldn't shadow a user's own.
  - **`<home>` comes from `os.UserHomeDir`,** and both directories under it are skipped when there's no home. `$HOME/.config` is used literally, on macOS too, rather than `os.UserConfigDir()`, whose macOS answer is `~/Library/Application Support`.
  - **A file the search can't check,** such as one in a directory it can't read, fails the load rather than being skipped. A path whose parent isn't a directory counts as absent.
  - **Errors name the found file's path,** which fixes the original's `reading config file ""`.
- **Parse failures keep no parser text.** Parsers quote what they choke on: JSON the offending character, dotenv the whole line, so a typo beside a secret would put the secret in the error. A `viper.ConfigParseError` becomes `ErrMalformedFile` with the format and, where the parser reports one, the position: go-toml's `Position()`, a JSON `SyntaxError` offset turned into a line and column, or the digits of a parser's `line N`. A format viper accepts but has no decoder for (viper 1.21 dropped `ini` and `hcl`, checked through `viper.NewCodecRegistry()`) says so instead of claiming the file is malformed.

### 8. The env table: leaves, map entries, names and collisions (→ docs/config.md)

The table maps every key to an env name.

**Walking T** with decision 1's rules:
- a field is a leaf unless its type is a struct or a map, and a text-decoded struct is a leaf;
- structs and pointers to structs recurse under their key, squashed embeds under the parent's key;
- a map field doesn't recurse by type. It takes its entry names from `fileKeys`: the segment after `<map key>.` in each file key. It adds the entry names in `Defaults`' value of the field, and recurses into the map's value type under `<map key>.<entry>`. A leaf value type makes the entry itself a leaf.

Entry leaves therefore come from the value type, not from what the file lists, so `MY_SVC_DATABASES_REPORTING_PASSWORD` works even when the file's entry sets only `host`. Entry names arrive lowercased from viper, a documented limitation.

**Names:** the env name is `PREFIX + "_" + key`, uppercased, with every character other than ASCII letters, digits and `_` replaced by `_`. One map from name to key holds every name and its `_FILE` twin. Inserting a name that's already taken by a different key fails with `ErrEnvNameCollision`, naming both keys. That catches `tls.key` beside `tls.key_file`, `http.read_timeout` beside `http_read.timeout`, and operator-supplied entries `acme-corp` beside `acme_corp`.

**Binding:** every entry is bound with `BindEnv(key, name)`. There's no `AutomaticEnv`, `SetEnvPrefix` or key replacer, so viper reads exactly the table's names, a section-named variable shadows nothing, and there's no second naming scheme to disagree with ours. Map fields themselves are never bound: binding both `databases` and `databases.reporting.host` makes viper's `AllSettings` depend on key order.

Alternatives:
- **`AutomaticEnv` for map entries.** It needs no table entries for them, but brings section shadowing back and leaves `_FILE` and collision checks blind to entries.
- **A double-underscore nesting separator** (`MY_SVC__HTTP__PORT`). It's unambiguous, but unconventional, and nothing detects a single-underscore typo.

### 9. `_FILE`: read once into viper's override layer (→ docs/config.md)

For each table entry, the loader looks up `X` and `X_FILE` with `os.LookupEnv`:
- **Both non-empty:** fail with `ErrEnvAndFileSet`, naming both variables.
- **`X_FILE` non-empty:** open the file and read it through `io.LimitReader` with a limit of 1 MiB plus one byte. More than 1 MiB fails with `ErrSecretFileTooLarge`. Trailing `\r` and `\n` are trimmed with `strings.TrimRight`, and the result goes in with `v.Set(key, value)`.
- **Otherwise:** `BindEnv`. Viper treats an empty `X` as unset by default, which matches the spec.

Read errors are wrapped as `reading X_FILE: %w`. `os.Open` errors carry the path, never the contents.

`Set` is viper's top layer, above bound env names. Because `X` and `X_FILE` are exclusive, that lands exactly at env priority. The loader never calls `os.Setenv`: that would put the secret back into the environment the feature exists to keep it out of, and mutate global state shared with other loads.

Rotation is out of scope. A module that needs a rotating credential takes a `*_path` field and re-reads it, the way Azure's SDK handles `AZURE_FEDERATED_TOKEN_FILE`.

### 10. Defaults: SetDefault per leaf, never decoding over a pre-filled struct (→ docs/config.md)

The loader walks `Defaults` with the same leaf rules and calls `SetDefault(key, value)` for every leaf reachable through non-nil values: slices as a whole, map entries one by one, text-decoded values as they are.

Each default is deep-copied before `SetDefault`, since viper keeps what it's given and decoding hands maps and slices in `any` fields straight through: without the copy, the loaded config would share memory with `Options.Defaults`. Viper resolves each key on its own, so a file's `hosts` replaces the default `hosts` entirely, and map entries merge entry by entry, as the spec requires. Leaves under a nil pointer get no default, so a nil section stays nil unless the file or the environment sets something in it. The zero `Defaults` sets only zero values, which is the same as having no defaults.

Alternative: decode file and env over a copy of `Defaults`. mapstructure truncates an existing slice to the input's length but reuses its elements by index, so a file's `hosts: [{name: x}]` over default `[{name: a, port: 1}, …]` would become `[{name: x, port: 1}]`, the default's port leaking into the file's host (a spike test pins this so the reason stays visible). Nil-pointer sections would also be pre-allocated by defaults that merely mention them.

### 11. Decoding hooks, in an order that matters

`viper.DecodeHook(ComposeDecodeHookFunc(durationHook, textHook, splitList))`, keeping viper's `WeaklyTypedInput` so env strings decode into numbers and bools:
- **`StringToTimeDurationHookFunc`.** `time.Duration` isn't a TextUnmarshaler.
- **A text hook wrapping `TextUnmarshallerHookFunc`.** On error it returns `cannot parse value as <type>` and drops the underlying error, which may contain the input.
- **`splitList`, our own.** It splits a string headed for a slice on commas and trims the spaces around each element, so `a, b` loads as a and b. It leaves a `[]byte` alone, so weak decoding takes the string's bytes as they are. mapstructure's `StringToWeakSliceHookFunc` split `[]byte` too, which made a key from a `_FILE` secret impossible, and it didn't trim.

The text hook must run before the slice hook. `net.IP` is a `[]byte` that implements `UnmarshalText`, and splitting it on commas first would break it.

`go-viper/mapstructure/v2` is a direct requirement at v2.4.0 or later, for the hooks and for the scrubbed error messages. A decoding failure is wrapped as `decoding config: %w`, and mapstructure's `DecodeError` names the dotted key. Tests assert that the value never appears.

### 12. Unknown keys: our own check, with UnmarshalExact as a backstop

A file key is known if it equals a table key, or extends one past `.` (a leaf of type `any` or a map value that's a leaf, holding nested file content). A struct with a `,remain` field accepts any key at its level. Unknown keys fail with `ErrUnknownKeys`, listing every unknown key in full and sorted (`htpp.port`, not mapstructure's `'' has invalid keys: htpp`).

When unknown keys aren't allowed, decoding also uses `UnmarshalExact`, so a gap in our check still fails, just with mapstructure's coarser message.

### 13. fx wiring

```go
func Provide[T any](o Options[T]) fx.Option  // fx.Module("configloader", fx.Provide(load))
type Params struct {
    fx.In
    Filename string       `name:"configloader:filename" optional:"true"`
    Logger   *slog.Logger `optional:"true"`
}
func AsFilename(v any) any                   // fx.Annotate(v, fx.ResultTags(...))
func WithFilename(path string) fx.Option     // fx.Supply(AsFilename(path))
```

- **Filename precedence:** the graph's filename, if supplied, then `o.Filename`, then the search.
- **Logger precedence:** the graph's `*slog.Logger`, which `log.Module` provides, then `o.Logger`, then a discarding logger. It's the plain `*slog.Logger` type, untagged: the app has one logger, and a tag would make every app wire it by hand.
- **No exported tag constant.** The constitution says callers never format tags, and `AsFilename` and `WithFilename` cover every way to supply one.
- **No `Module` variable,** because a generic provider can't be one. That's the constitution exception in the proposal.

**`fx.Out` fan-out (→ docs/config.md):** if T embeds `fx.Out`, fx provides each field instead of T, so `server.Config` and `logger.Config` reach their modules without adapter functions. The embedded `Out` field has no exported fields, so it contributes no keys and no env names. Two fields of the same type need `name` tags. A test confirms it works end to end.

### 14. Constitution edits (first PR)

- **Config vs developer-time wiring:** the line "Never use the required tag…" is replaced. Tags check shape and presence, `required` included. A mode-dispatching Config marks each mode-specific field `validate:"-"`, and its `Validate` delegates to the active one with `configvalidator.ValidateField`, returning the result directly or joined. Cross-field invariants stay in `Validate() error`, returning exported sentinels.
- **Config fields:** a Config never has both `x` and `x_file`, and a module's own path fields are named `*_path`.
- **fx integration:** `config/configloader`'s generic provider is the one exception to the `Module` variable rule.
- **Nested module layout.** A nested module lives at `<group>/<group><name>`, and its package name equals its directory name: `config/configloader`, and later `http/httpserver`. Outside the filesystem path and the tag, which the go command needs, the package name is the module's identity:
  - **Tags:** the versioning example becomes `http/httpserver/v0.2.0`.
  - **fx.Module names:** the package name (`"logger"`, `"configloader"`). Today's colon-separated path form would be redundant, as in `"http:httpserver"`.
  - **Name and group tags:** the `<module>` segment is the package name, as in `name:"configloader:filename"`.
  - **Spec folders:** the package name, `openspec/specs/configloader`. Today's dash-flattened path would be redundant, as in `config-configloader`.
  - **Commit scope:** the package name for a module (`feat(configloader): …`), and the directory for a tool (`tools/release`).
  - **The root README's tag example** follows (`http/httpserver/v0.2.0`).

### 15. Testing

- **config/configvalidator:** one test per spec scenario, with fixture types in `configvalidator_test`. Assertions use `errors.AsType[*configvalidator.Error]`, `errors.AsType[*configvalidator.RuleError]`, and `Errors` for multiple violations.
- **config/configloader:**
  - environment through `t.Setenv`, so no `t.Parallel`;
  - files in `t.TempDir()`, the home directory through `t.Setenv("HOME", …)`, the working directory through `t.Chdir`, and `SearchPaths` for the rest. Nothing reads the real `/etc`.
- **Spike tests that pin library behaviour the design relies on:**
  - viper lowercases map keys;
  - default slices are replaced, not index-merged;
  - map entries merge across defaults and file;
  - text-decoded defaults survive the round trip;
  - `net.IP` decodes as text;
  - errors don't contain values.
- **fx:** `fxtest.New` with `configloader.Provide`, `configloader.WithFilename` and `fx.Populate`, plus the `fx.Out` case.

### 16. Dependencies

- **go-playground/validator/v10 (`config/configvalidator`).** Declarative shape checks are the constitution's chosen style. Hand-rolling tag parsing and evaluation means rebuilding a mature library with a large catalogue (url, hostname, oneof, cidr, dive). It brings locales, universal-translator, mimetype, go-urn and golang.org/x/crypto and x/text. Every module with a Config pays that, still far less than viper's tree.
- **spf13/viper (`config/configloader`).** Kept by decision in explore over koanf. File formats, layered lookup and search are fiddly and solved. The split confines its tree to apps.
- **go-viper/mapstructure/v2 (`config/configloader`).** Viper's own decoder, required directly for the hooks and the scrubbed errors (decision 11).
- **go.uber.org/fx (`config/configloader`).** For `Provide`. `config/configvalidator` doesn't import fx.
- **testify (both).** Tests only.
- **pelletier/go-toml/v2 (`config/configloader`).** Already viper's TOML parser, now required directly so a parse failure's position can be read from its `DecodeError` instead of its message (decision 7). It adds nothing to the module graph.
- **No logging dependency (`config/configloader`).** It logs through `log/slog`'s `*slog.Logger`, taken from the caller (decision 17).

### 17. Logging through an injected `*slog.Logger` (→ docs/config.md)

The loader reports where its values came from through a `*slog.Logger` it's given: `Options.Logger` standalone, or `Params.Logger` from the graph with fx, where `log.Module` provides one. Without one, records go to `slog.DiscardHandler`, so the loader works without `log`.

**It doesn't import `log`.** The constitution has modules log through an injected `*slog.Logger` and integrate with a sibling through a value, never an import. `log/slog` is the standard library, so `configloader`'s `go.mod` gains nothing.

**No dependency cycle.** `log.New` takes only developer wiring (debug, quiet, format, destination), never a Config. The logger therefore exists before the config is loaded, which is what `log`'s README promises.

| When | Level | Message | Attributes |
|------|-------|---------|------------|
| A config file is read, given or found by the search | Info | loaded config file | path |
| The search finds no file | Info | no config file found | searched (the directories) |
| A key is set from its env variable | Debug | config key set from environment | key, env |
| A key is read through its file variable | Debug | config key read from file | key, env, path |
| A file key is ignored because unknown keys are allowed | Warn | ignoring unknown config key | key |

- **Values never appear,** the same rule as for errors, since config holds secrets. Attributes name keys, env variables and paths.
- **Failures aren't logged.** They're the returned error, and logging them too would report them twice; the caller decides what to do with an error.
- **Attributes are typed** (`slog.String`), as the constitution and sloglint's attr-only require. The searched directories are a `[]string`, which has no typed constructor, so that one is `slog.Any`.
- **The Info line answers the search's open question,** which file was actually read, on every start. The Warn line turns `AllowUnknownKeys` from silently ignoring typos into flagging them.
- **No context parameter.** Loading happens before any request exists, and sloglint here doesn't require context-aware calls, so `Load` keeps its signature.

Alternatives:
- **Never logging,** the first draft's non-goal. Without a logger that could exist before the config, the loader would have needed a report value threaded back to the caller. `log` removed that problem.
- **Importing `log` for its logger.** It would tie `configloader`'s releases to `log`'s, for a type the standard library already defines.
- **A required logger under fx.** Every app would then need `log.Module`, or its own logger, just to load config.

## Risks / Trade-offs

- **[A type that embeds something with a Validate method gets it promoted and becomes a Validator itself, so the walk stops there and skips its other fields.]** → A test pins the behaviour, and the README and docs warn about embedding validated types. The alternative, ignoring promoted methods, would break legitimate embedding.
- **[Tag rules on struct elements inside slices and maps run only with `dive`.]** → The README says so, with an example. The walk still calls element Validators.
- **[Keys compose only through direct returns and joins.]** → Godoc and docs give the rule, and a test shows the opaque case.
- **[`ValidateField` panics when its field isn't beneath its parent.]** → It's a programming error in one mode's branch, found by the first test that exercises that mode.
- **[A Validate method is skipped while tags fail in its branch, so semantic errors can surface one run later.]** → Accepted. Tags catch the shape errors that would make the method's report misleading.
- **[The search tries every format viper decodes in each place, `config.env` included.]** → A file only counts when it's named for the place: `config` in a directory of the app's own, `.<name>` in the working directory. Strict unknown keys turn most wrong-file pickups into errors.
- **[Viper lowercases map keys.]** → Documented, and pinned by a test so a viper change is noticed.
- **[Map-entry collisions come from operator content and fail at deploy time, not in tests.]** → The error names both keys.
- **[A mapstructure regression could put values back into error messages.]** → The minimum version is pinned, and tests assert a planted value never appears.
- **[`_FILE` uses viper's override layer, which would also beat command-line flags if flags were ever bound.]** → No flags are bound (non-goal in explore). Revisit if that changes.
- **[Exporting the key-rule helpers widens the validator's API.]** → Two small functions, documented as the shared key rules. The alternative is drift.
- **[Loads read the process environment, so loader tests can't run in parallel.]** → Accepted. The same holds for any `t.Setenv` test.

- **[A type that embeds a nil pointer to a Validator and declares its own Validate is skipped too.]** → Reflection can't tell a declared method from a promoted one, so the walk treats both alike when the pointer is nil. The README advises against declaring `Validate` on such a type, and a test pins the behaviour.

## Migration Plan

Nothing in this repo uses either module yet, so nothing migrates.

1. **First PR:** `config/configvalidator` at 0.1.0, the constitution edits, its README row and `use ./config/configvalidator` in `go.work`. Merging publishes `config/configvalidator/v0.1.0`.
2. **Second PR:** `config/configloader` at 0.1.0, requiring `config/configvalidator v0.1.0`, plus its README row, `use ./config/configloader` and `docs/config.md`. CI builds it with `GOWORK=off` against the published `config/configvalidator`. This is the first real test of the dependency-tier rule.

Each pull request's title and body are drafted for approval before it's opened. After each merge, publish should create the module's tag and GitHub release, and the module should resolve through proxy.golang.org with `GOWORK=off`. These are checks on CI and the release tooling, not tasks, so they don't hold the change back from archiving.

Rollback means fixing forward: 0.1.1, or 0.2.0 for an API fix, plus a `retract` directive if a published version is unusable.

## Open Questions

- **The exact wording of rule failures** (`fails min=1024`, or something closer to prose). It changes no spec, since the spec only requires the rule and its parameter.
- **A constitution line preferring text-decoded field types** (`slog.Level`, `netip.Prefix`) over strings parsed in resolvers. It's worth proposing once a ported module needs one.

## Implementation Notes

What changed while applying the first pull request, and why.

- **Delegation needed a second export.** The spec's mode-dispatch scenario expects `messaging.amqp.url`, but composing at the Validator's own key (this decision 4's first draft) gives `messaging.url`. The original had the same gap. `ValidateField(parent, field)` was chosen over pointer identity and author-named keys; decision 4, decision 14, the spec and tasks 5.1, 5.2, 6.1 and 6.2 were updated.
- **mapstructure reads `mapstructure:"-"` as a key named `-`**, contrary to decision 1's assumption. The rules still treat such a field as having no key (see Context). The validator, which reaches such fields through hand-built values, keys them by Go name.
- **The tag-pass translation follows values, not types** (decision 2), for interface fields and map entries whose names contain brackets or dots.
- **The walk descends interface values** (decision 3, rule 5), copying their contents like map values, to match go-playground's tag pass. The spec now lists interface values in both requirements.
- **Fixed in review:** `Validate` on a `T` of interface type panicked in the tag pass, which took the address of a value it had reached through the interface. The tag pass now hands go-playground the struct value. `ValidateField` couldn't find a field behind an interface field holding a pointer; it now looks through interfaces as the walk does.
- **`plan` selects every module, not just the new one.** Tasks 8.1 and 18.1 (since dropped, below) expected `plan` to print only the new module, but registering it changes `go.work`, which selects every module for lint and test (release-ci spec). `check` still covers only the new module. The tasks were corrected.
- **`Errors` replaced `errors.Join`** at the user's request, after the first draft of the pull request. Validate and ValidateField return it for any number of violations, which also settles the open question of a helper to list them. The constitution's receiver rule gained an exception for slice error types.
- **Open for the second pull request: `,remain`.** The loader must recognise a `,remain` field, which takes no env names and makes its level accept any key. The shared key rules don't cover it, so either the loader reads that tag option itself, or `config/configvalidator` gains a helper, which after `v0.1.0` means a 0.2.0 release first.

- **Release steps left the task list.** Sections 8 and 18 (local `plan` and `check`, opening each pull request, the published release and a consumer check) were dropped, and the sections after 8 renumbered. A new constitution rule keeps steps outside the branch out of task lists, since an unticked one would hold the change back from archiving. The Migration Plan keeps the checks worth making.

- **The loader logs.** Once `log/v0.1.0` shipped, with a logger that needs no config, the "never logs" non-goal became decision 17: the loader reports which file it read, which keys came from the environment, and which unknown keys it ignored, through an optional injected `*slog.Logger`. The loader spec gained a requirement, and tasks 10.1, 12.3 and 14 cover it.

- **mapstructure truncates a pre-filled slice.** Decision 10 said decoding `[x]` over `[a b c]` gives `[x b c]`; v2.5.0 gives `[x]`, but still decodes each element over the old one at its index, so struct elements merge field by field. The decision stands; its reason and the spike test now use struct elements.

- **`,remain` is read by the loader itself.** It checks the tag option beside `configvalidator.KeySegment`, so `config/configvalidator` needed no 0.2.0 release first. A `,remain` field takes no env names and makes its level accept any file key.
- **Unknown-key matching is narrower than decision 12.** A file key beneath a leaf counts as known only when the leaf has interface type (`any`, or a map whose values are `any`), so `http.port.number` is reported as unknown rather than left to fail in decoding. A file key naming a struct or map section (`http: 5`) counts as known, so it fails in decoding with a type error instead of being reported as unknown.
- **Keys are lowercased in the table,** as viper stores them, so an untagged field's key is its Go name lowercased in env names, log records and unknown-key errors. Validation errors still use the Go name, as `config/configvalidator` writes it.
- **docs/config.md points at the change directory** (`openspec/changes`) rather than a file path, since archiving moves the design. Repoint it at the archived design.md when archiving.

- **The search looks for app-named files, most specific place first** (decision 7). At the user's request, after the second review: `.<name>.<ext>` in the working directory, and `config.<ext>` in `$HOME/.config/<name>/`, `$HOME/.<name>/` and `/etc/<name>/`, in that order after `SearchPaths`. Viper's search couldn't express a different file name per place, so the loader searches itself, and the spike pinning `ConfigFileUsed` after viper's search was dropped.

## Fixes from code review

The review of `add-configvalidator` at bc73835 found fifteen issues. All are fixed, each with a regression test, and the spec, README and decisions 3 and 4 above were updated to match.

- **Invalid config getting through.**
  - `prefix` split any error with `Unwrap() []error`, so `fmt.Errorf` with several `%w` verbs lost its text, and a custom join with no parts reported nothing. → Only `Errors` and `errors.Join`'s type split, recognised by type rather than by comparing messages (decision 4).
  - A slice, array or map passed to `Validate` or `ValidateField` had its elements' Validators called but none of their tags checked. → Each element is checked as if validated alone.
  - A tag failure in entry `acme.eu` suppressed `acme`'s Validator, because keys were compared as string prefixes. → Paths of segments (decision 3).
  - The walk skipped unexported embedded structs, so Validators inside were never called, and `ValidateField` panicked on a field promoted from one. → Both enter embedded structs, exported or not.
- **Panics.**
  - A typed-nil `*Error` from a Validator, or an `*Error` with a nil `Err`, panicked. → A nil `*Error` is no violation, and a nil `Err` reads as "invalid".
  - A Validate method promoted from a nil embedded pointer panicked when called. → Such a struct isn't treated as a Validator. Recovering the panic was rejected: it's fragile, and nil dereferences hang this development machine (go1.27.2, darwin/arm64) whether recovered or not, inside or outside the sandbox, so the fix couldn't depend on one.
  - `ValidateField` couldn't find a field behind a map of pointers. → It looks through map values.
- **Wrong keys.**
  - `KeySegment` gave an unexported squashed embed a segment, though mapstructure decodes its fields at the parent's level. → Squash is checked before exportedness for embedded fields.
  - `ValidateField` on a nil field reported "config is nil" at the parent's key. → It names the field when that's unambiguous.
  - Entry names holding `]`, or starting with `[`, were mis-parsed or lost their dot. → Paths, and namespace resolution that tries each `]` in turn until the rest of the namespace fits.
  - Typed map keys were named by their `String` method. → Their text form or underlying value.
- **Order and efficiency.**
  - Tag violations under maps came out in random order. → Sorted by path: fields by declaration, elements by index, entries by name.
  - The walk reflected over every element of scalar slices, and resolving a map entry rescanned the map for each failure. → Containers that can't hold a Validator are passed over, and each map is indexed once per tag pass.
- **Design.** Keys flattened into strings early and re-parsed caused three of the above, plus the squash over-suppression decision 3 had accepted. → Positions stay segment lists until a `Key` is written, which removed that accepted trade-off.

## Fixes from code review (second pull request)

The review of `add-configloader` found these: two from reading the diff, the rest from an independent review that confirmed each by running it. Each is fixed with a regression test.

- **`Provide` changed the `Options` its closure captured,** so the same `Provide` option reused in a second app inherited the first app's filename and logger. → The constructor works on a copy.
- **`envName` uppercased before replacing characters,** and uppercasing some characters outside ASCII yields ASCII letters (`ß` gives `SS`), so an entry named `straße` read `…_STRASSE` instead of the spec's `…_STRA_E`. → Characters are replaced first.
- **A null file key hid the environment beneath it.** A section left empty in YAML (`http:` with its children commented out) reads as null, and viper's key flattening takes it for a leaf that shadows every key beneath it, env-bound ones included, so `MY_SVC_HTTP_PORT` was read but never decoded, and still logged as set. → The file layer is rebuilt from `AllSettings`, which skips nulls, right after it's read.
- **A bad duration leaked part of its value.** mapstructure's duration hook cuts `time: invalid duration "…"` at the last space, so `hunter two` left `"hunter` in the error. → The duration hook is wrapped like the text hook (decision 11), naming only the type.
- **A config type that contains itself recursed forever.** → The walk stops at a struct type it's already inside; that key takes no env names beneath it and accepts any file key there, leaving decoding to check them.
- **An unexported embedded pointer with `,squash` panicked inside mapstructure,** which can't set it. → `Load` rejects that config type with an error.
- **Default map keys differing only in case gave a random result,** since both lowercase to one key and map iteration order picked the survivor. → They fail with `ErrEnvNameCollision`. Default entries are now named by their key's text form, as `config/configvalidator` names entries, rather than by `fmt.Sprint`, which a `String` method could change.
- **A default map entry named `""` or holding a dot broke decoding,** since viper reads a dot as nesting and an empty entry replaces the whole map. → `Load` rejects it. Entries from the file have the same viper limitation, now documented.
- **Documented, not changed:** mapstructure always allocates a squashed pointer embed, so a nil section stays nil only when it isn't squashed; and names that normalise alike (`my-svc`, `my_svc`) share an env prefix.

A second review, of `ef32d45`, found eight more. Each is fixed with a regression test, and the spec and README were updated where behaviour changed.

- **A plain `[]byte` field couldn't load from a string.** mapstructure's weak-slice hook split it on commas, then failed to decode each piece as a byte. → `splitList` leaves byte slices to weak decoding (decision 11).
- **Parse errors quoted the file.** JSON's error quoted the offending character, and dotenv's quoted the whole line. → `ErrMalformedFile` with the format and position only (decision 7).
- **The loaded config shared memory with `Options.Defaults`.** A map or slice reached through an `any` field went to viper, and from there into the result, by reference. → Defaults are deep-copied before `SetDefault` (decision 10).
- **Comma-separated elements kept their spaces.** `a, b` loaded as `a` and ` b`. → `splitList` trims each element.
- **A name could start with a digit,** giving env names no shell can export. → `ErrInvalidName`, and the spec's name rule says so.
- **Mixed-case default map keys came back lowercased** without the docs saying so. → Documented in the README and `Options.Defaults`; it's viper's key lowercasing.
- **An unreadable default was skipped without an error.** Reaching it actually panicked first, in `entryName`, on a map key read through an unexported embedded field. → `entryName` doesn't assert on a key it can't read, and an unreadable default fails the table build.
- **`walkEntries` repeated a root-key special case.** → The prefix is computed once.
- **Found while fixing:** the loader's `entryName` fell back to `fmt.Sprint`, so a key type with a display-only `String` method was named differently from `configvalidator`'s keys. → It follows `configvalidator`'s rule: text form, then underlying value. The two copies stay in step by hand until `configvalidator` exports the rule, which would need its own minor release first.

