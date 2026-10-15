# fx-modules

Reusable Go modules I reach for day to day, built around [Uber fx](https://uber-go.github.io/fx/). Each subdirectory is an independently versioned Go module, so importing one never pulls in the dependencies of another.

## Modules

Nothing has shipped yet. Each module gets a row here, in alphabetical order, as it lands. Import paths follow `github.com/sikhlana/fx-modules/<dir>`.

| Module | Description |
|--------|-------------|

## Installation

Import only what you need:

```bash
go get github.com/sikhlana/fx-modules/<module>@latest
```

## How a module is shaped

fx is the integration layer, not a requirement. Every module works both ways:

```go
// With fx: list the module and supply what it needs.
app := fx.New(
    mymodule.Module,
    fx.Supply(mymodule.Config{Address: "localhost:6379"}),
    fx.Invoke(func(c *mymodule.Client) { /* ... */ }),
)

// Standalone: constructors take a plain Params struct.
c, err := mymodule.New(mymodule.Params{Config: cfg, Logger: logger})
```

Each module's README documents its Configuration, its Exported Members, and both usage styles. The conventions every module follows (how types are exposed, what goes in `Config` and what's wired in code, how long-running loops opt in to running) live in [openspec/config.yaml](./openspec/config.yaml).

## Versioning and releases

This is a multi-module repository. Each module has its own `go.mod`, `README.md` and `VERSION` file, and is tagged on its own as `<module>/v<version>` (for example `logger/v0.1.0`, or `http/server/v0.2.0` for a nested module).

Versions follow [semver](https://semver.org/):

- **Patch** (`0.1.0` to `0.1.1`): bug fixes, internal refactors, dependency updates, doc and test changes.
- **Minor** (`0.1.0` to `0.2.0`): new exported types, functions or methods.
- **Major** (`1.2.0` to `2.0.0`): breaking changes to a 1.x module. Before 1.0, breaking changes take a minor bump. A v2+ module's import path gains a `/vN` suffix, so majors are a last resort.

Release flow:

1. Change a module and bump its `VERSION` in the same PR.
2. Merge to `main`. CI creates the `<module>/v<version>` tag and a GitHub release.

Tags are never created by hand.

## Adding a module

1. Create the directory with a `go.mod` for `github.com/sikhlana/fx-modules/<dir>`, a `VERSION` containing `0.1.0`, and a `README.md`.
2. Register it in the root `go.work`.
3. Add a row to the [Modules](#modules) table.
4. Follow the layout and conventions in [openspec/config.yaml](./openspec/config.yaml).

## Working in this repo

Changes are spec-driven with [OpenSpec](https://github.com/Fission-AI/OpenSpec): each module's behavioural guarantees live in `openspec/specs/<module>/spec.md`, and changes are proposed, applied and archived under `openspec/changes/`. [CLAUDE.md](./CLAUDE.md) is the entry point for AI agents.

## License

[MIT](./LICENSE)
