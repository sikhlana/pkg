Both modules are new, so they start at 0.1.0 with no bump tasks, and `cli` isn't touched. Neither has mocked interfaces, so there are no go generate tasks. Nothing is copied from go-modules' `configloader`: code, godoc and comments are written here, from what this code does.

The change ships as two pull requests (design.md, Migration Plan). Sections 1 to 7 are the first, `config/configvalidator`; sections 8 to 16 are the second, `config/configloader`, started only after `config/configvalidator/v0.1.0` is published. Opening the pull requests and checking the releases aren't tasks; the Migration Plan covers them.

## 1. Validator module scaffold

- [x] 1.1 Branch `add-configvalidator` off `origin/main` after `git fetch`.
- [x] 1.2 Create `config/configvalidator/`:
  - `go.mod` for `github.com/sikhlana/pkg/config/configvalidator`, with the go directive from `.go-version`;
  - package `configvalidator`, with tests in `configvalidator_test` (design decision 2);
  - `VERSION` containing `0.1.0`;
  - require the latest go-playground/validator/v10 and stretchr/testify.
  - Done when `go build ./...` passes from `config/configvalidator/`.
- [x] 1.3 Register `./config/configvalidator` in the root `go.work`.

## 2. Key rules

- [x] 2.1 The two exported key-rule helpers from design decision 1:
  - a struct field to its key segment: mapstructure name, Go name when untagged, no segment when squashed, excluded when tagged `-` or unexported;
  - whether a type decodes itself from text, directly or through a pointer.
- [x] 2.2 Tests:
  - tagged, untagged, squashed with and without a name, `-`, unexported, and tag options such as `,omitempty` ignored;
  - text decoding for `time.Time`, `netip.Prefix`, `slog.Level`, `net.IP`, a type with a pointer-receiver `UnmarshalText`, a plain struct and a plain string.

## 3. Errors

- [x] 3.1 `Error` (`Key`, `Err`) and `RuleError` (tag and parameter) with pointer receivers, `Errors` (a `[]error` with value receivers), and `ErrNilConfig`, as in design decisions 2 and 4:
  - `Error` renders `key: cause`, or the cause alone when the key is empty, and unwraps to `Err`;
  - `RuleError` renders `fails tag=param`, or `fails tag`;
  - `Errors` renders each violation on its own line and unwraps to the violations.
- [x] 3.2 The unexported composition from design decision 4: a key is prefixed onto a top-level `*Error`, onto each part of a joined error, and anything else becomes an opaque `*Error` at that key. The input is never mutated.
- [x] 3.3 Tests:
  - rendering with and without a key, and an `Errors` of one and of two violations;
  - `errors.AsType` finds the `Errors`, the first `*Error` and its `*RuleError` through a caller's wrapping;
  - composition of a single error, a joined error, a nested join, and an opaque `fmt.Errorf("…: %w", …)` wrapper;
  - the input is unchanged after composition;
  - `errors.Is` reaches a sentinel through `Error` and through a join.

## 4. Tag pass

- [x] 4.1 The package-level validator, built with required-struct validation on. Each failure's Go-name namespace is translated into a key with the section 2 rules: the root segment is dropped, and `[i]` and `[key]` are kept. Each failure becomes an `*Error` whose cause is a `*RuleError`. See design decision 2.
- [x] 4.2 Tests for the spec requirements "Tag rules apply to every reachable field except skipped ones", "Violations name the config key" and "Tag violations never contain the value":
  - a nested failure's key;
  - a dormant struct behind `validate:"-"` with an empty `required` field passes, and the same struct validated on its own fails;
  - a nil pointer section is skipped;
  - a squashed leaf's key is flattened, an untagged field uses its Go name, and an anonymous embed without `,squash` keeps a segment;
  - dived elements give `hosts[2]` and `databases.reporting`;
  - a planted value never appears in a message, and `oneof=read write` and `min=12` do.

## 5. Validator walk

- [x] 5.1 `Validator` and `Validate[T any](cfg *T) error`, as in design decision 3:
  - a nil config returns `ErrNilConfig`;
  - the tag pass runs, then the walk;
  - the walk stops at nil pointers, `validate:"-"` fields and text-decoded values;
  - it calls the outermost Validator, through the value or its address, unless a tag violation sits at or below its key;
  - it descends into struct fields, slice and array elements (`[i]`), map values (`.entry`) and interface values. Map values and interface contents are copied so pointer receivers are found;
  - the result is nil or an `Errors`, never an empty one.
  - `ValidateField[P, F any](parent *P, field *F) error`: finds field beneath parent by address, ignoring validate tags, and returns `Validate(field)` with each violation under field's key. A nil field returns `ErrNilConfig`; a field not beneath parent panics.
- [x] 5.2 Tests for the spec requirements "A Validate method owns its branch", "A Validate method doesn't run on a branch whose tag rules failed", "Every violation is reported", "Delegated violations keep their full key" and "A nil config is an error":
  - the outermost method wins, and the walk finds methods below a plain parent;
  - a pointer receiver on the root, on slice elements and on map values;
  - a skipped field hides its Validate method;
  - a text-decoded type's own Validate is called, and its insides aren't searched;
  - a shape failure suppresses the semantic check, while another branch's failure is still reported;
  - violations in two sections are both reported, a sentinel stays matchable, and a valid config returns nil;
  - a root failure renders the reason alone;
  - mode dispatch through `ValidateField` reports `messaging.amqp.url` with the key named once, with value and pointer receivers; a method-built keyed error becomes `http.ssl.cert_path`; and several delegated violations each get the parent's key;
  - `ValidateField` keys beneath a skipped field, behind a pointer, in a slice element and in a squashed field; a field outside its parent, the parent itself and a nil parent panic; a nil field returns `ErrNilConfig`;
  - interface values are walked, and a nil interface or a nil pointer inside one is skipped;
  - a promoted Validate method from an embedded type stops the walk, pinning the behaviour from the design's Risks;
  - a nil config.

## 6. Constitution and docs (first pull request)

- [x] 6.1 In `openspec/config.yaml`, make design decision 14's four edits:
  - replace the `required` ban with the dispatch convention: tags check shape and presence, mode-specific fields are marked `validate:"-"`, and the parent's Validate delegates with `configvalidator.ValidateField`, returning the result directly or joined;
  - a Config never has both `x` and `x_file`, and a module's own path fields are named `*_path`;
  - `config/configloader`'s generic provider is the exception to the `Module` variable rule.
  - the nested module layout: `<group>/<group><name>` with the package named after the directory, and the package name used for fx.Module names, the `<module>` segment of name and group tags, spec folders and commit scopes. The versioning example becomes `http/httpserver/v0.2.0`.
- [x] 6.2 Write `config/configvalidator/README.md` in the constitution's order:
  - overview;
  - Configuration: none, and why;
  - Exported Members: every export, including `Errors`, `Error.Key`, `Error.Err`, the `RuleError` fields, the methods, `ErrNilConfig`, `ValidateField` and both key-rule helpers;
  - Usage, Standalone: validating a hand-built Config; a mode-dispatching Validate method with `validate:"-"` that delegates with `ValidateField`; `dive` for element rules; the embedding caveat;
  - Usage, With fx: a Provider closure validating the Config it's handed (the module itself doesn't import fx).
  - No backticks in table cells.
- [x] 6.3 In the root `README.md`, add the `config/configvalidator` row to the Modules table, in alphabetical order, and change the tag example `http/server/v0.2.0` to `http/httpserver/v0.2.0`.

## 7. Verification (first pull request)

- [x] 7.1 In `config/configvalidator/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 7.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 7.1 and 7.2.
- [x] 7.3 Review the branch's diff for correctness. Fix findings in new commits, record them in design.md, and repeat 7.1 and 7.2.

## 8. Loader module scaffold

- [x] 8.1 Once `config/configvalidator/v0.1.0` is published, `git fetch`, then branch `add-configloader` off `origin/main`.
- [x] 8.2 Create `config/configloader/`:
  - `go.mod` for `github.com/sikhlana/pkg/config/configloader`, with the go directive from `.go-version`;
  - package `configloader`, with tests in `configloader_test` (design decision 2);
  - `VERSION` containing `0.1.0`;
  - require the published `github.com/sikhlana/pkg/config/configvalidator v0.1.0`, the latest spf13/viper, go-viper/mapstructure/v2 at v2.4.0 or later, go.uber.org/fx and stretchr/testify.
  - Done when `go build ./...` passes from `config/configloader/`, both with the workspace and with `GOWORK=off`.
- [x] 8.3 Register `./config/configloader` in the root `go.work`.

## 9. Spike tests on library behaviour

Done first, because the design relies on these behaviours (design decision 15). If any contradicts design.md, stop and update the design and specs before continuing.

- [x] 9.1 Tests against viper and mapstructure directly, each kept as a pinning test:
  - map keys come back lowercased;
  - a default slice is replaced by the file's;
  - map entries merge across defaults and the file;
  - decoding over a pre-filled slice merges by index (the reason for design decision 10);
  - `time.Time` and `netip.Prefix` defaults survive `SetDefault`;
  - `net.IP` decodes through the text hook when it runs before the weak-slice hook;
  - `BindEnv(key, name)` reads `name` verbatim, and without `AutomaticEnv` a section-named variable shadows nothing;
  - `ConfigFileUsed` returns the path of the file the search found (later dropped: the loader searches itself, see Implementation Notes).

## 10. Options, name and file

- [x] 10.1 `Options[T]`, including `Logger` (nil discards, design decision 17), and the name rule (`ErrInvalidName`, prefix derivation), as in design decisions 5 and 8.
- [x] 10.2 Finding the file, as in design decision 7:
  - an explicit path, with errors wrapped as `reading config file "<path>"`;
  - the search over `SearchPaths`, `/etc/<name>`, `<home>/.<name>` (skipped when there's no home directory) and `.`, with errors naming `ConfigFileUsed()`;
  - an Info record naming the file read, or saying none was found with the directories searched (design decision 17).
- [x] 10.3 Tests for the spec requirements "The name sets the environment prefix", "An explicit config file must exist" and "Without one, a config file is searched for":
  - `my-svc` gives `MY_SVC_`, and an empty name and `../etc` both fail;
  - an explicit file that's missing (matching `fs.ErrNotExist`), malformed or of an unsupported format fails, naming the path;
  - an extra directory is searched before the working directory (`t.Chdir`), and `$HOME/.<name>` is found (`t.Setenv("HOME", …)`);
  - no file anywhere loads from the environment;
  - a malformed file found by the search fails, naming its full path.

## 11. Env table, collisions and unknown keys

- [x] 11.1 The table, as in design decision 8:
  - the walk over T with the key rules;
  - map entries from the file's keys and from `Defaults`, recursing into the value type;
  - env names (uppercased, with characters other than ASCII letters, digits and `_` replaced by `_`);
  - one name map holding every name and its `_FILE` twin, failing with `ErrEnvNameCollision` and naming both keys.
- [x] 11.2 The unknown-key check from design decision 12: `ErrUnknownKeys` lists full keys, sorted. A level with a `,remain` field accepts any key. When `AllowUnknownKeys` is set, each unknown key is logged at Warn instead (design decision 17).
- [x] 11.3 Tests for the spec requirements "Env names never collide" and "Unknown keys in the file fail the load":
  - `tls.key` beside `tls.key_file`, `http.read_timeout` beside `http_read.timeout`, and map entries `acme-corp` beside `acme_corp` each fail, naming both keys;
  - a typo fails, naming `htpp.port`, and succeeds when unknown keys are allowed;
  - map entries never count as unknown.

## 12. Layers: defaults, environment and `_FILE`

- [x] 12.1 Defaults: `SetDefault` for every leaf of `Defaults` reachable through non-nil values (design decision 10).
- [x] 12.2 The environment, as in design decision 9. For each table entry:
  - `X` and `X_FILE` both set fails with `ErrEnvAndFileSet`;
  - a `_FILE` read goes through a 1 MiB limit (`ErrSecretFileTooLarge`), trims trailing `\r` and `\n`, and goes in with `Set`;
  - otherwise the entry is bound with `BindEnv(key, name)`;
  - each key that's actually set from its variable, or read through its file variable, is logged at Debug with the key, the variable and any path, never the value (design decision 17).
  - No `AutomaticEnv`, env prefix or key replacer.
- [x] 12.3 Tests for the spec requirements "Defaults, then the file, then the environment", "Every key is settable from the environment", "Only known env names are read", "Secrets can come from files" and "Loading has no side effects":
  - precedence: default only, file over default, environment over file, a slice replaced, map entries merged;
  - a nested leaf with no default or file entry, a squashed leaf, and text-decoded leaves, all set from the environment;
  - a map entry overridden, a map entry gaining a field the file omits, and a map entry that can't be created;
  - a section-named variable and an unknown prefixed variable change nothing;
  - `_FILE`: trailing newline stripped, other whitespace kept, taking priority over the config file, a non-string field, both set, an empty `_FILE` variable, a missing file, and an oversized file whose contents never appear in the error;
  - the process environment is unchanged after a `_FILE` load, and two loads under different names don't affect each other.

## 13. Decoding and validation

- [x] 13.1 The decode hooks in order (duration, scrubbed text, weak slice), as in design decision 11. Decode with `UnmarshalExact` unless unknown keys are allowed, into a zero T, wrapping errors as `decoding config: %w`. Then `configvalidator.Validate`, wrapped as `validating config: %w`. Return the zero T on every failure (design decision 6).
- [x] 13.2 Tests for the spec requirements "Values decode by type" and "The loaded config is validated":
  - a duration, a comma-separated slice, and `slog.Level` and `netip.Prefix` from the file;
  - a bad number fails, naming `http.port` and an integer type, and a bad text-decoded value fails; neither error contains the value;
  - an invalid `http.port` gives a violation reachable with `errors.AsType[*configvalidator.Error]`;
  - a dormant mode struct loads;
  - the zero value comes back on failure.

## 14. fx wiring

- [x] 14.1 `Provide` (wrapped in `fx.Module("configloader", …)`), `Params`, `AsFilename` and `WithFilename`, as in design decision 13. The filename comes from the graph first, then `Options.Filename`, then the search. `Params.Logger` is an optional, untagged `*slog.Logger` that wins over `Options.Logger`.
- [x] 14.2 Tests under `fxtest.New` for the spec requirement "An fx app loads its config once, from the path it supplies":
  - a supplied path;
  - no path, with the search finding a file through `SearchPaths`;
  - a failed load failing the app's start;
  - a T embedding `fx.Out` providing each of its fields.
- [x] 14.3 Tests for the spec requirement "Loading is reported through the caller's logger", with a handler capturing records:
  - one test per record in design decision 17's table, checking level, message and attributes;
  - a value planted in the environment, the file and a `_FILE` variable never appears in any record;
  - a failed load returns its error and logs nothing about it;
  - no logger writes nothing;
  - under `fxtest.New`, a supplied `*slog.Logger` receives the records.

## 15. Docs (second pull request)

- [x] 15.1 Write `config/configloader/README.md` in the constitution's order:
  - overview;
  - Configuration: none of its own, since it loads the app's, and why;
  - Exported Members: every export, including the `Options` fields, `Params.Filename`, `Params.Logger` and the sentinels;
  - Usage, Standalone: `Load` with defaults, the search, env names, `_FILE`, map entries, unknown keys, and the lowercased map keys;
  - Usage, With fx: `Provide` with `cli`'s `--config` passed through `WithFilename`, `log.Module` supplying the logger, and the `fx.Out` fan-out;
  - what the loader logs, at which level, and that values never appear.
  - No backticks in table cells.
- [x] 15.2 Write `docs/config.md` from the design decisions marked "→ docs/config.md": why the module is split, the dispatch convention, layering with slice and map semantics, env naming and collisions, `_FILE` and rotation, the `fx.Out` fan-out, and what the loader logs.
- [x] 15.3 In the root `README.md`, add the `config/configloader` row to the Modules table, in alphabetical order.

## 16. Verification (second pull request)

- [x] 16.1 In `config/configloader/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 16.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 16.1 and 16.2.
- [x] 16.3 Review the branch's diff for correctness. Fix findings in new commits, record them in design.md, and repeat 16.1 and 16.2.
