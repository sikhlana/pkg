## Why

`cli`'s `--config` flag carries a path that nothing reads, and every stateful module still to come has a Config that the constitution says is validated where it's loaded. go-modules' `configloader` does both jobs in one module, which is its main flaw: modules that only validate their own Config inherit viper's dependency tree (dialer, ssh, mysql and others list viper, fsnotify and afero in their go.mod for that reason alone). It's rebuilt here as two modules rather than copied, and the rebuild fixes gaps the original carries: env bindings silently missing for struct-shaped leaves such as `time.Time`, typos in the config file ignored, viper leaking into the public API, and a `required` ban that every module author pays for.

## What Changes

- New module `config/configvalidator` (`github.com/sikhlana/pkg/config/configvalidator`), starting at 0.1.0. It checks a Config the way every module and the loader share:
  - **A tag pass, then a Validator walk.** go-playground `validate` tags run first; then `Validate()` is called on the outermost Validator in each branch, which owns its subtree.
  - **Every violation is reported,** not just the first.
  - **Errors carry the config key.** An error type pairs the dotted mapstructure key with the cause and renders as `key: reason`. Keys compose when a parent's `Validate` delegates to a child through a helper that takes both, so a delegated failure still names its full key.
  - **Tag failures name the rule and its parameter,** never the value, since config values include secrets.
  - **The walk treats types that implement `encoding.TextUnmarshaler` as single values,** the same rule the loader uses for env names.
- New module `config/configloader` (`github.com/sikhlana/pkg/config/configloader`), starting at 0.1.0 after `config/configvalidator` is published. It reads configuration into a struct with viper:
  - **Files.** An explicit file (from `cli`'s `--config`, say) must exist. Without one, it searches for a `config` file in `/etc/{name}/`, `$HOME/.{name}/` and `./`, and finding none is fine.
  - **Environment.** The prefix is the name uppercased with `-` replaced by `_`. Every struct leaf, and every map entry the file or the defaults define, gets an env name and a `_FILE` name, bound explicitly. Two keys mapping to the same name is an error naming both. An empty name is an error.
  - **`_FILE` indirection.** `X_FILE` names a file whose contents become the value of `X`, read once at load, size-capped, with trailing newlines stripped. Setting both `X` and `X_FILE` is an error; an empty `X_FILE` counts as unset. Errors name the variable and the path, never the contents.
  - **Precedence:** typed defaults, then the file, then the environment.
  - **Strict by default.** Unknown keys in the file fail the load unless the caller allows them.
  - **Decoding** handles durations, comma-separated slices and any `encoding.TextUnmarshaler` (`slog.Level`, `netip.Prefix`, `time.Time`).
  - **Validation.** The loaded value goes through `config/configvalidator` before it's returned.
  - **fx.** A generic provider loads a type into the graph, and the config file path is developer wiring under a name tag, with `AsFilename` and `WithFilename`.
- How it differs from go-modules' `configloader`:
  - Split in two; modules depend only on `config/configvalidator`.
  - Validate takes a pointer, checked at compile time, and reports every violation. The error type has `Key`, not `Field`, no built-in "validating configuration" prefix, and no nil-receiver tolerance.
  - Defaults are a typed value of the config struct instead of a `func(*viper.Viper)`, so viper stays out of the API.
  - `AutomaticEnv` and strcase are gone. Env names are computed and bound explicitly, collisions are detected, and map entries from the file become env-overridable.
  - New: `_FILE` indirection, strict unknown keys, `TextUnmarshaler` leaves.
  - Fixed: an error that read `reading config file ""` when a searched file failed to read.
- Constitution:
  - **The `required` ban is lifted.** A mode-dispatching Config marks each mode-specific field `validate:"-"`, and its `Validate` delegates to the active one, so the tag pass never reaches a dormant struct.
  - **A Config never has both `x` and `x_file`;** a module's own path fields are named `*_path`. `_FILE` indirection would make the pair collide.
  - **`config/configloader` is an exception to the `Module` variable rule,** because its provider is generic over the config type.
  - **Nested modules live at `<group>/<group><name>`,** with the package named after the directory (`config/configloader`, like `net/http/httptest`). The package name then becomes the module's fx.Module name, spec folder and commit scope, and the tag example becomes `http/httpserver/v0.2.0`.
- New `docs/config.md`: why the module is split, the dispatch convention, the layering, getting sections of the app's Config into modules with `fx.Out`, and why rotating secrets take a path field instead of `_FILE`.
- Root README gains two Modules rows and the new tag example, and `go.work` registers both modules.
- Delivered in two pull requests: `config/configvalidator` with the constitution edits, then `config/configloader` once `config/configvalidator/v0.1.0` is published. It's the first real test of the rule that siblings release in dependency order.

## Capabilities

### New Capabilities

- `configvalidator`: the tag pass and Validator walk, the outermost-validator rule, key paths and their composition through delegation, reporting every violation, and what errors may contain.
- `configloader`: file search and explicit files, env naming and binding (including map entries), `_FILE` indirection, precedence, typed defaults, strict unknown keys, decoding, validation on load, and fx wiring of the file path.

### Modified Capabilities

None. `cli` keeps its `--config` contract, and `release-ci` runs its first sibling-dependency release without any requirement changing.

## Impact

- **`config/configvalidator` (new module):** all of its exported API is new. Its exposure shape is plain functions, an interface and an error type. It has no fx Module, no Config and no runner.
- **`config/configloader` (new module):** all of its exported API is new. Its exposure shape is a generic plain function that loads a type, and a generic fx option that provides it, with developer wiring for the file path. It has no `Module` variable (the constitution exception above), no Config of its own and no runner.
- **`cli`:** unchanged. Apps pass its `--config` value to the loader.
- **`tools/release`:** unchanged, now exercised on a module that requires a sibling.
- **New dependencies:**
  - `config/configvalidator`: go-playground/validator/v10.
  - `config/configloader`: spf13/viper (and its tree), go-viper/mapstructure/v2 for decode hooks, go.uber.org/fx, and `config/configvalidator`.
  - Both: testify, for tests only.
- **Docs and repo files:** `openspec/config.yaml` (the four constitution edits), `docs/config.md` (new), `README.md` (two Modules rows, the tag example), `go.work` (`use ./config/configvalidator`, `use ./config/configloader`).
- **Release:** merging the first PR publishes `config/configvalidator/v0.1.0`; merging the second publishes `config/configloader/v0.1.0`.
