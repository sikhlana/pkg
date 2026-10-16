## 1. kernel 0.2.0: config becomes opt-in

Sections 1 to 3 are the first pull request, `feat(kernel)!:`. See design decisions 1 to 3 and 6.

- [x] 1.1 Branch `kernel-config-option` off `origin/main` after `git fetch`.
- [x] 1.2 Bump `kernel/VERSION` from 0.1.0 to 0.2.0: a minor, for the new exports and the breaking change before 1.0.
- [x] 1.3 `ConfigFlag` and `Options.Config func(path string) fx.Option`. `RootCommand` adds `--config` only when `Config` is set and the root doesn't already define it, with the usage ``"path to the config `file`"`` and no shorthand.
- [x] 1.4 `Run`: look for `--config` only when `Config` is set, read it by `ConfigFlag`, and append `Config(path)` where `configloader.WithFilename` was. Remove the `configloader` import, then `go mod tidy`, so `kernel`'s go.mod no longer requires it.
- [x] 1.5 Tests, with a closure recording the path in place of the loader, so nothing imports `configloader`:
  - `--config ./app.yaml` reaching `Config`, and an empty path without the flag;
  - `--config` in help with its placeholder when `Config` is set;
  - a program without `Config` running without `ErrMissingFlag`, with nothing of `Config` in its app;
  - `cli` 0.2.1's root, which still defines `--config`, reused rather than redefined.

## 2. kernel 0.2.0 docs and verification

- [x] 2.1 `kernel/README.md`:
  - `Options.Config` and `ConfigFlag` in Exported Members;
  - `--config` in the flags table, present only with `Config`;
  - the example loading config through `Config`, with `Filename: path`;
  - the migration from `configloader.Provide` in `Shared`;
  - no `WithFilename` collision note.
  - No backticks in table cells.
- [x] 2.2 The root `README.md`'s `kernel` row: config loading through `Options.Config`, not `--config` into the loader.
- [x] 2.3 `docs/kernel.md`: the dependency diagram without `config/configloader`, and the config wiring through `Options.Config`.
- [x] 2.4 In `kernel/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`. Confirm `GOWORK=off go build ./...` and `go mod tidy -diff` are clean, with no `configloader` requirement.
- [x] 2.5 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 2.4 and 2.5.

## 3. kernel 0.2.0 code review fixes

The review of step 1's working tree found eight issues. Six are fixed below and two needed no change. Design decision 3 and the kernel spec record the outcome.

- [x] 3.1 While cli's root defines `--config`, `RootCommand` adjusts it: the kernel's help, with its `file` placeholder, when `Config` is set; hidden when it isn't, with `Run` rejecting a passed `--config` before any app is built (`errNoConfig`, unexported). A program without `Config` no longer accepts and ignores the flag, and one upgraded with its loader still in `Shared` fails as soon as it's run with `--config`. Tests: help hides the flag without `Config`, and `--config` is rejected without `Config`.
- [x] 3.2 A nil option from `Config` is skipped, as a nil `Shared` is, instead of panicking in `fx.New`. Test: a `Config` returning nil runs.
- [x] 3.3 The placeholder test asserts `--config file` exactly, now true in this release, instead of a pattern cli's `string` also matched.
- [x] 3.4 The reuse test skips, saying why, once cli's root no longer defines `--config`, instead of passing without testing reuse.
- [x] 3.5 `docs/kernel.md` says a program without `Config` doesn't take `--config`, true now and after cli 0.3.0; the README's notes on the transition and the migration describe the hiding and the rejection.
- [x] 3.6 No change for the reused flag's type: `RootCommand` always builds on `cli.NewRootCommand`, whose `--config` is a string flag. No change for tying `ConfigFlag` to `cli.ConfigFlag`: referring to it is what step 2 has to make unnecessary; the constant's comment notes the shared name.
- [x] 3.7 Repeat 2.4 and 2.5: lint reports 0 issues, `go test ./...` and `go test -race ./...` pass in `kernel/`, `GOWORK=off` builds and tidies clean with no `configloader` requirement, and the `-short` sweep passes across all six modules.

## 4. cli 0.3.0: the root stops carrying --config

Sections 4 to 6 are the second pull request, `feat(cli)!:`, and wait for the first to merge, so `kernel` on main no longer refers to `cli.ConfigFlag`. See design decisions 4 and 5.

- [x] 4.1 Once the first pull request has merged, branch `cli-drop-config-flag` off `origin/main` after `git fetch`.
- [x] 4.2 Bump `cli/VERSION` from 0.2.1 to 0.3.0, a minor for the breaking change before 1.0, and `config/configloader/VERSION` from 0.1.0 to 0.1.1, a patch for docs.
- [x] 4.3 `NewRootCommand` no longer adds `--config`, `ConfigFlag` is deleted, and the godoc says the root claims no single-letter shorthand.
- [x] 4.4 `cli` tests: the config-flag test goes; the root has no `--config`, so `--config` is an unknown flag; a subcommand's own `-c` still works.
- [x] 4.5 `cli/README.md`: Configuration without the `--config` sentence, the `NewRootCommand` row without the flag, no `ConfigFlag` row, and the Standalone example without reading the config path.
- [x] 4.6 `config/configloader`: the README's `WithFilename` row and With fx example stop reading `cli`'s flag, showing `kernel`'s `Options.Config` with `Filename: path` and a hand-defined flag for programs without `kernel`. `WithFilename`'s godoc stops naming `cli`'s flag.

## 5. cli 0.3.0 verification

- [x] 5.1 In `cli/` and `config/configloader/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 5.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 5.1 and 5.2.

## 6. cli 0.3.0 code review fixes

The review of step 2's working tree found five issues. Four are fixed below; one, the main specs still describing the old `--config` until the archive, is left for step 3 as planned.

- [x] 6.1 `NewRootCommand`'s godoc, the README row and the delta spec say what's true: the root adds no flags of its own, beyond cobra's `--help` (`-h`) and `--version` (`-v`, which cobra drops when another flag takes `v`). The new requirement is named "The root adds no flags of its own"; the `-c` test still covers the module claiming no letters.
- [x] 6.2 `config/configloader`'s example for programs without `kernel` defines `--config` with `PersistentFlags`, so subcommands accept it, as they did with cli 0.2.1.
- [x] 6.3 `kernel` 0.2.0's paths for cli's flag are exercised with `GOWORK=off go test ./...` in `kernel/`, against the published cli v0.2.1: the workspace now resolves cli 0.3.0, where the reuse test skips, and CI doesn't test `kernel` in this pull request. The reuse test runs and passes there.
- [x] 6.4 The help test drops its `NotContains("--config")`; `TestRootHasNoConfigFlag` stays, as the spec's scenario.
- [x] 6.5 Repeat 5.1 and 5.2, plus 6.3: lint reports 0 issues in `cli/` and `config/configloader/`, their full tests pass, `kernel`'s tests pass with `GOWORK=off`, and the `-short` sweep passes across all six modules.

## 7. kernel 0.2.1: require cli 0.3.0

Section 7 is the third pull request, `refactor(kernel):`, and waits for `cli/v0.3.0` to be published, since `kernel`'s go.mod requires it.

- [x] 7.1 Once `cli/v0.3.0` is published, branch `kernel-cli-0.3` off `origin/main` after `git fetch`.
- [x] 7.2 Bump `kernel/VERSION` from 0.2.0 to 0.2.1: a patch, for internals and a dependency.
- [x] 7.3 Require `cli` v0.3.0, and drop the adjusting of cli's flag: `RootCommand`'s check for an existing `--config`, the help swap and the hiding, and `Run`'s rejection with `errNoConfig`. `ConfigFlag`'s comment stops mentioning cli's flag.
- [x] 7.4 Tests: the reuse test goes; a program without `Config` rejects `--config ./app.yaml` before building an app, now as cobra's unknown flag; help without `Config` still shows no `--config`.
- [x] 7.5 `kernel/README.md`: drop any note about `cli`'s flag between releases.
- [x] 7.6 In `kernel/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`. Confirm `GOWORK=off go build ./...` and `go mod tidy -diff` are clean.
- [x] 7.7 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 7.6 and 7.7.

## 8. kernel 0.2.1 code review fixes

The review of step 3's working tree found four low-severity issues and no correctness bugs. All are fixed below.

- [x] 8.1 `kernel/README.md` says that from 0.2.1 the kernel requires cli 0.3.0, so updating the kernel updates cli too, and a command built straight from `cli.NewRootCommand` that read cli's `--config` defines its own flag. The version stays a patch: the kernel's API is unchanged, and dependency updates are patches.
- [x] 8.2 The README's migration section says a stray `--config` fails "naming the flag" rather than "as an unknown flag", true for 0.2.0's rejection and 0.2.1's cobra error alike.
- [x] 8.3 The unknown-flag test asserts that the error names `--config`, the exit code and that the app didn't run, rather than cobra's exact wording.
- [x] 8.4 One comment above both flags explains the backquoted placeholder, replacing the duplicate on `--config`.
- [x] 8.5 Repeat 7.6 and 7.7: lint reports 0 issues, `go test ./...` and `go test -race ./...` pass in `kernel/`, `GOWORK=off` builds, tests and tidies clean, and the `-short` sweep passes across all six modules.
