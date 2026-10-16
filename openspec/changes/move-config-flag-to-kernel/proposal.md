## Why

`cli`'s root command defines `--config` but never reads it; the only code that turns the path into config is `kernel.Run`, which hands it to `configloader.WithFilename`. That split leaves every kernel-built program showing `--config` even when it loads no config, and makes `kernel` import `configloader` for that one call, so viper and its tree are built into every app, config or not. Now that `kernel` composes a program's commands and owns its other flags, `--config` belongs beside its only consumer, and only in programs that load config.

## What Changes

- **`kernel` (0.2.0): config becomes opt-in. BREAKING.**
  - **`Options.Config`,** a function given `--config`'s path, empty when the operator passed none, that returns the program's config loading as an fx option. Nil means the program loads no config: no `--config` flag and nothing in the graph.
  - **`--config` (`kernel.ConfigFlag`)** is added only when `Config` is set, with a value placeholder in help. While `cli`'s root still defines it, the kernel reuses that flag.
  - **`kernel` no longer imports `config/configloader`.** viper leaves the build of every app that doesn't load config, and `kernel`'s go.mod drops it.
  - **Migration:** a program that put `configloader.Provide` in `Options.Shared`, relying on the kernel to supply `WithFilename`, moves it into `Config` and passes the path as `Options.Filename`. Left in `Shared`, it still compiles, but `--config` no longer reaches it.
- **`cli` (0.3.0): the root stops carrying `--config`. BREAKING.**
  - `NewRootCommand` no longer adds `--config`, and `ConfigFlag` is deleted.
  - The root still claims no single-letter shorthand.
  - An app that bumps `cli` to 0.3.0 has to bump `kernel` to 0.2.0 or later with it: `kernel` 0.1.0 refers to `cli.ConfigFlag`.
- **`config/configloader` (0.1.1):** its README's examples stop reading `cli`'s flag, and show the kernel's `Config` and a hand-defined flag instead.
- **`kernel` (0.2.1):** requires `cli` 0.3.0 and drops the reuse of `cli`'s flag.
- `docs/kernel.md` follows: the kernel's dependencies no longer include `configloader`, and its wiring paragraph explains `Options.Config`.
- Delivered in three pull requests, one module each, in this order: `kernel` 0.2.0, then `cli` 0.3.0 with `configloader` 0.1.1, then `kernel` 0.2.1 once `cli/v0.3.0` is published. The `configloader` README edit rides in the second, since its examples change with `cli`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernel`: the root command carries `--config` only when the program loads config, and a command's app loads config only then, with `--config`'s path.
- `cli`: the root command no longer carries `--config`; the promise that it claims no single-letter shorthand stays, as a requirement of its own.

## Impact

- **`kernel`:** exported API changes. `Options.Config` and `ConfigFlag` are new. `Run` no longer supplies `configloader.WithFilename`, and the root carries `--config` only with `Config`. The second release changes only internals and its `cli` requirement.
- **`cli`:** exported API changes. `ConfigFlag` is removed, and `NewRootCommand` no longer adds a flag.
- **`config/configloader`:** README only; no API change.
- **`config/configvalidator`, `kernelrun`, `log`, `tools/release`:** unchanged.
- **Dependencies:** `kernel` drops `config/configloader`, and with it viper's tree. Nothing is added.
- **Docs:** the `kernel`, `cli` and `configloader` READMEs, `docs/kernel.md`, and the root README's `kernel` row, which mentions `--config` into the loader.
- **Release:** the three merges publish `kernel/v0.2.0`, `cli/v0.3.0` with `config/configloader/v0.1.1`, and `kernel/v0.2.1`.
