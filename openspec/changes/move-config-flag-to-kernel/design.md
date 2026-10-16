## Context

See proposal.md for why `--config` moves; the requirements are in specs/kernel/spec.md and specs/cli/spec.md. This document covers how, and the order the three releases have to go in.

Facts that shape the approach, checked against the code on main:

- **`kernel.go` imports `config/configloader` for one call,** `configloader.WithFilename(flags.config)` in `Run`, and reads the path through `cli.ConfigFlag`.
- **`configloader.Options.Filename` already means "the file to read; empty means search",** so a path handed to the program reaches the loader without `WithFilename`.
- **`kernel` v0.1.0 requires `cli` v0.2.1,** whose `NewRootCommand` always adds `--config`. pflag panics when a flag set defines a name twice.
- **Every go.mod must require published versions of its siblings,** and the `go.work` sweep builds each module against its siblings' working trees. A change that breaks a sibling's build in the workspace fails the sweep, even though CI, which builds with `GOWORK=off`, stays green.
- **No module other than `kernel` reads `cli.ConfigFlag`.** The `configloader` and `cli` READMEs show it in examples.

## Goals / Non-Goals

**Goals:**

- `--config` exists in a program only when the program loads config, and only the code that loads config depends on `configloader`.
- Every release on the way builds and passes the workspace sweep.

**Non-Goals:**

- A `configloader` helper returning the function `Options.Config` takes. The closure is one line; a helper can follow if a missed `path` turns out to be a real trap.
- Changing `configloader`'s API, or `WithFilename`, which stays for programs wiring the loader by hand.
- Moving `--version` or anything else out of `cli`'s root.

## Decisions

### 1. Options.Config is a function of the path (→ docs/kernel.md)

```go
type Options struct {
	// Config returns the program's config loading as an fx option, given --config's path, which is empty when the
	// operator passed none. Nil means the program loads no config: there's no --config flag and nothing in the graph.
	Config func(path string) fx.Option

	// … Format, TextHandler, StartTimeout, StopTimeout, Shared as before
}
```

```go
app := kernel.New(kernel.Options{
	Config: func(path string) fx.Option {
		return configloader.Provide(configloader.Options[Config]{Name: "mytool", Filename: path})
	},
})
```

- **Rejected: `Config fx.Option`, with the kernel still supplying `WithFilename`.** It keeps the loader out of the graph when unset, but `kernel` still imports `configloader`, so every app still builds viper.
- **Rejected: the kernel generic over the config type** (`kernel.New[Config]`). It ties every program to `configloader`'s options through the kernel's signature, and a program without config would have to name a type it doesn't have.
- **Rejected: leaving the flag to each program.** It's the duplication the kernel exists to remove, and the help text and placeholder would drift between programs.
- **What the function shape costs:** a closure that ignores `path` silently ignores `--config`. The README's example passes it through, and the kernel's tests show it reaching the loader.

`Run` appends `Config(path)` after the logger's wiring and before `Options.Shared`, where `WithFilename` sat. The loader can then report through the app's logger, and the program's shared modules can take the config.

### 2. The flag

- **`kernel.ConfigFlag`** is `"config"`, a persistent string flag added by `RootCommand` only when `Options.Config` is set.
- **No shorthand,** keeping `cli`'s stance that the program's commands keep every letter. `-q` stays the only letter the kernel claims.
- **Help names its value** with a backquoted placeholder, ``"path to the config `file`"``, so it reads `--config file`.
- **`Run` checks for the flag only when `Config` is set,** so a program without config never fails with `ErrMissingFlag` over it.

### 3. kernel 0.2.0 reuses cli's flag while cli still defines it

`kernel` 0.2.0 must require a published `cli`, which is still 0.2.1, whose root defines `--config`. If `RootCommand` defined it again, pflag would panic. So 0.2.0's `RootCommand` adjusts cli's flag rather than defining its own, and `Run` reads it by `kernel.ConfigFlag` either way, since both name `"config"`:

- **with `Config` set,** cli's flag gets the kernel's help, so it shows its `file` placeholder;
- **without it,** cli's flag is hidden from help, and `Run` rejects it if the operator passed it, before any app is built, naming `--config`.

The first implementation left cli's flag as it was, which review found let a program without `Config` accept `--config` and ignore it. That's worse than a leftover: a program upgraded from 0.1.0 with its loader still in `Shared` would quietly stop honouring `--config`. With the rejection, that program fails as soon as it's run with `--config`, and runs as before without it. Step 1 then behaves like the end state, except that the rejection's message comes from the kernel rather than from cobra's unknown-flag error.

The rejection's error is unexported. It's an operator error that cobra prints, not one callers branch on, and step 3 removes it; dropping an exported sentinel in a patch would trip gorelease's floor.

| kernel | cli | Result |
|---|---|---|
| 0.1.0 | 0.2.1 | today |
| 0.2.0 | 0.2.1 | adjusts cli's flag: the kernel's help with `Config`, hidden and rejected without |
| 0.2.0 | 0.3.0 | defines its own flag, only with `Config` |
| 0.1.0 | 0.3.0 | doesn't compile: `cli.ConfigFlag` is gone |
| 0.2.1 | 0.3.0 | defines its own flag, with no adjusting left |

`kernel` 0.2.1 drops the adjusting (the help swap, the hiding and the rejection) once `cli` 0.3.0 is published. The constant isn't tied to `cli.ConfigFlag` in code, since referring to it is what step 2 has to make unnecessary.

### 4. cli deletes ConfigFlag rather than deprecating it

A deprecated constant naming a flag the root no longer defines would turn every `GetString(cli.ConfigFlag)` into a runtime error. Deleting it turns it into a compile error, which is what a pre-1.0 breaking change should be. The `cli` spec's `--config` requirement goes. Its other promise, that the root claims no single-letter shorthand, becomes a requirement of its own with the `-c` scenario.

### 5. Three pull requests, one module each

```
1. feat(kernel)!: …      kernel 0.2.0                       requires cli 0.2.1
2. feat(cli)!: …         cli 0.3.0, configloader 0.1.1      README only for configloader
3. refactor(kernel): …   kernel 0.2.1                       requires cli 0.3.0, once published
```

- **The kernel goes first,** so the workspace sweep stays green at every step. Removing `cli.ConfigFlag` first would break `kernel`'s build in the workspace until the kernel caught up.
- **Steps 1 and 2 could share a pull request,** since the reuse check keeps `kernel` working against both `cli` versions. Keeping one module per pull request keeps each commit's scope and `!` exact.
- **`configloader`'s README edit rides with `cli`,** because its examples change with `cli`'s flag.
- **The change is archived with step 3.**

### 6. Migration for kernel 0.1.0 programs

A program with `configloader.Provide` in `Options.Shared` keeps compiling with `kernel` 0.2.0, but the kernel no longer supplies `WithFilename`, so `--config` stops reaching the loader, which falls back to searching. The fix is to move the loader into `Config` and pass `path` as `Options.Filename`. Step 1's pull request is marked `!` and its body and the README spell this out. `kernel` 0.1.0 has no consumers outside this repo's author.

### 7. Testing

- **`kernel` 0.2.0:**
  - `--config` present with `Config`, with its placeholder in help, and its path, or an empty one, reaching `Config`. A recording closure stands in for the loader: if the tests imported `configloader`, `go mod tidy` would keep it in `kernel`'s go.mod as a test dependency.
  - a program without `Config` running without `ErrMissingFlag`, and nothing of `Config` in its app;
  - the reuse check, through `cli` 0.2.1's own root, which still defines `--config`;
  - the go.mod no longer requiring `configloader` (`go mod tidy -diff`).
- **`cli` 0.3.0:** the root has no `--config`, and a subcommand's own `-c` flag still works.
- **`kernel` 0.2.1:** the reuse test goes with the check. A new test shows that `--config` is an unknown flag in a program without `Config`, which only holds once `cli` 0.3.0 no longer defines it.

## Risks / Trade-offs

- [A program left with the loader in `Shared` stops honouring `--config`] → Run with `--config`, it fails, naming the flag (decision 3); `!` on step 1 and the migration in its pull request body and README cover the rest.
- [`Config` returning a nil option] → Skipped, as a nil `Shared` is.
- [A closure that ignores `path` silently ignores `--config`] → The documented example passes it; a `configloader` helper can follow if it bites.
- [An app bumping `cli` to 0.3.0 while on `kernel` 0.1.0 doesn't compile] → Step 2's pull request body says to bump `kernel` with it.
- [Between steps 1 and 2, cli's `--config` still exists on every root] → Hidden and rejected without `Config` (decision 3); step 2 removes it.

## Migration Plan

Three pull requests, in the order of decision 5. Step 3 waits for `cli/v0.3.0` to be published, since `kernel`'s go.mod requires it. The merges publish `kernel/v0.2.0`, `cli/v0.3.0` with `config/configloader/v0.1.1`, and `kernel/v0.2.1`. go-modules is unaffected.
