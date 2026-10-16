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
- **Restricting the search to some formats.** The search stays as decided, trying every extension viper supports. Strict unknown keys catch most wrong-file pickups.
- **Logging.** The loader returns errors and never logs, so it has no logger dependency.
- **Viper versions without `BindEnv(key, name)` or `ConfigFileUsed`.** Both exist in the viper versions we'd require.

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
4. A Validator, through either the value or its address, is called, unless a tag violation's key equals its key or extends it past `.` or `[`. Either way the walk stops there.
5. Otherwise the walk descends into exported struct fields, slice and array elements (`key[i]`), map values (`key.<entry>`) and non-nil interface values (at the same key), as go-playground's tag pass does.

Map values and interface contents aren't addressable, so each is copied into a new addressable value before the Validator check, which lets pointer-receiver methods be found.

Skipping a Validator whose branch failed its tags (rule 4) keeps "report everything" from calling methods on data their own tags rejected, such as an unknown mode. It over-suppresses slightly around squashed embeds: a squashed struct shares its parent's key, so a sibling's tag failure under that key also suppresses it. Accepted, since squashed embeds rarely carry their own Validate.

Alternatives:
- **Fail-fast like the original.** It's simpler, but every violation costs a redeploy.
- **Run every Validator regardless of tag failures.** It produces duplicate and misleading reports.

### 4. Reporting: Error, errors.Join, and composition through direct returns

Each violation is an `*Error{Key, Err}` with pointer receivers. `Error()` returns `key: <cause>`, or just the cause when the key is empty, and `Unwrap` returns `Err`. Validate returns nil or an `Errors`, a named `[]error` holding one `*Error` per violation, even when there's only one. `Errors` has value receivers, as `go/scanner.ErrorList` does, so callers range over it; its `Error()` puts each violation on its own line, and its `Unwrap() []error` lets `errors.Is` and `errors.AsType` check each one. It's never returned empty. A nil config returns `ErrNilConfig`.

When the walk wraps what a Validator returned, at key K:
- an `*Error` at the top becomes a new `*Error` with key `K.<its key>`; the original is never mutated;
- a joined error is unwrapped through `Unwrap() []error`, and each part is wrapped the same way;
- anything else becomes `*Error{Key: K, Err: err}`.

A keyed error buried under `fmt.Errorf("context: %w", …)` is opaque: the context text survives, and the key stops at K. The original composed through any wrapping with `errors.As`, which silently dropped the wrapper's text.

**Delegating to a field.** K is the Validator's own key, so composition alone can't name the field it delegates to: `Validate(&m.AMQP)` returns `url`, and the walk makes `messaging.url`. The original had the same gap. A second export closes it:

```go
func ValidateField[P, F any](parent *P, field *F) error
```

It finds `field` beneath `parent` by address, through struct fields, non-nil pointers and slice and array elements, ignoring validate tags, and builds its key with decision 1's rules. It returns `Validate(field)` with each violation prefixed by that key, so `ValidateField(m, &m.AMQP)` returns `amqp.url` and the walk makes `messaging.amqp.url`. Both pointers come from the same receiver, so it works with value receivers too: `&m` and `&m.AMQP` both point into the copy. A nil field returns `ErrNilConfig`. A field that isn't beneath the parent, the parent itself included, panics: it's a bug in the method, not a config problem, and the first test of that mode finds it.

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
    SearchPaths      []string // searched before the defaults
    Defaults         T        // typed defaults; the zero value means none
    AllowUnknownKeys bool
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
- **Search:** `SetConfigName("config")`, then `AddConfigPath` for each directory in `SearchPaths`, `/etc/<name>`, `<home>/.<name>` and `.`, in that order. Viper tries every supported extension in each directory before moving to the next.
  - `<home>` comes from `os.UserHomeDir`, and that directory is skipped when there's no home. That avoids viper expanding `$HOME` to `/.<name>`.
  - `ConfigFileNotFoundError` means no file, which is fine.
  - Any other error is wrapped with `ConfigFileUsed()`, the found file's real path. That fixes the original's `reading config file ""`.

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

Viper resolves each key on its own, so a file's `hosts` replaces the default `hosts` entirely, and map entries merge entry by entry, as the spec requires. Leaves under a nil pointer get no default, so a nil section stays nil unless the file or the environment sets something in it. The zero `Defaults` sets only zero values, which is the same as having no defaults.

Alternative: decode file and env over a copy of `Defaults`. mapstructure reuses an existing slice by index, so default `[a b c]` overlaid with `[x]` would become `[x b c]` (a spike test pins this so the reason stays visible). Nil-pointer sections would also be pre-allocated by defaults that merely mention them.

### 11. Decoding hooks, in an order that matters

`viper.DecodeHook(ComposeDecodeHookFunc(durationHook, textHook, weakSliceHook))`, keeping viper's `WeaklyTypedInput` so env strings decode into numbers and bools:
- **`StringToTimeDurationHookFunc`.** `time.Duration` isn't a TextUnmarshaler.
- **A text hook wrapping `TextUnmarshallerHookFunc`.** On error it returns `cannot parse value as <type>` and drops the underlying error, which may contain the input.
- **`StringToWeakSliceHookFunc(",")`.**

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
    Filename string `name:"configloader:filename" optional:"true"`
}
func AsFilename(v any) any                   // fx.Annotate(v, fx.ResultTags(...))
func WithFilename(path string) fx.Option     // fx.Supply(AsFilename(path))
```

- **Filename precedence:** the graph's filename, if supplied, then `o.Filename`, then the search.
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

## Risks / Trade-offs

- **[A type that embeds something with a Validate method gets it promoted and becomes a Validator itself, so the walk stops there and skips its other fields.]** → A test pins the behaviour, and the README and docs warn about embedding validated types. The alternative, ignoring promoted methods, would break legitimate embedding.
- **[Tag rules on struct elements inside slices and maps run only with `dive`.]** → The README says so, with an example. The walk still calls element Validators.
- **[Keys compose only through direct returns and joins.]** → Godoc and docs give the rule, and a test shows the opaque case.
- **[`ValidateField` panics when its field isn't beneath its parent.]** → It's a programming error in one mode's branch, found by the first test that exercises that mode.
- **[A Validate method is skipped while tags fail in its branch, so semantic errors can surface one run later.]** → Accepted. Tags catch the shape errors that would make the method's report misleading.
- **[The search may pick up any supported extension, `config.env` or `config.ini` included.]** → Accepted with search. Strict unknown keys turn most wrong-file pickups into errors.
- **[Viper lowercases map keys.]** → Documented, and pinned by a test so a viper change is noticed.
- **[Map-entry collisions come from operator content and fail at deploy time, not in tests.]** → The error names both keys.
- **[A mapstructure regression could put values back into error messages.]** → The minimum version is pinned, and tests assert a planted value never appears.
- **[`_FILE` uses viper's override layer, which would also beat command-line flags if flags were ever bound.]** → No flags are bound (non-goal in explore). Revisit if that changes.
- **[Exporting the key-rule helpers widens the validator's API.]** → Two small functions, documented as the shared key rules. The alternative is drift.
- **[Loads read the process environment, so loader tests can't run in parallel.]** → Accepted. The same holds for any `t.Setenv` test.

## Migration Plan

Nothing in this repo uses either module yet, so nothing migrates.

1. **First PR:** `config/configvalidator` at 0.1.0, the constitution edits, its README row and `use ./config/configvalidator` in `go.work`. Merging publishes `config/configvalidator/v0.1.0`.
2. **Second PR:** `config/configloader` at 0.1.0, requiring `config/configvalidator v0.1.0`, plus its README row, `use ./config/configloader` and `docs/config.md`. CI builds it with `GOWORK=off` against the published `config/configvalidator`. This is the first real test of the dependency-tier rule.

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
- **`plan` selects every module, not just the new one.** Tasks 8.1 and 18.1 expected `plan` to print only the new module, but registering it changes `go.work`, which selects every module for lint and test (release-ci spec). `check` still covers only the new module. The tasks were corrected.
- **`Errors` replaced `errors.Join`** at the user's request, after the first draft of the pull request. Validate and ValidateField return it for any number of violations, which also settles the open question of a helper to list them. The constitution's receiver rule gained an exception for slice error types.
- **Open for the second pull request: `,remain`.** The loader must recognise a `,remain` field, which takes no env names and makes its level accept any key. The shared key rules don't cover it, so either the loader reads that tag option itself, or `config/configvalidator` gains a helper, which after `v0.1.0` means a 0.2.0 release first.
