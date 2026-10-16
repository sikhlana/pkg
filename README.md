# pkg

Reusable Go modules I reach for day to day, built around [Uber fx](https://uber-go.github.io/fx/). Each subdirectory is an independently versioned Go module, so importing one never pulls in the dependencies of another.

## Modules

Each module gets a row here, in alphabetical order, as it lands. Import paths follow `github.com/sikhlana/pkg/<dir>`.

| Module | Description |
|--------|-------------|
| [cli](./cli) | Cobra scaffolding for a program's main: signal handling, exit codes, root and version commands, build info |

## Installation

Import only what you need:

```bash
go get github.com/sikhlana/pkg/<module>@latest
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
- **Major** (`1.2.0` to `2.0.0`): breaking changes to a 1.x module. Before 1.0, breaking changes take a minor bump. A v2+ module's import path gains a `/vN` suffix, so majors are a last resort: v2 of `logger` is a new module in `logger/v2`, starting at `2.0.0` and tagged `logger/v2.0.0`, as the go command expects.

CI sets a floor under the bump: [gorelease](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease) compares the module's exported API with its last release and suggests the lowest version that change allows. `VERSION` can go higher (a behaviour change behind an unchanged API still deserves a minor), never lower.

Release flow:

1. Change a module and bump its `VERSION` in the same PR. A new module starts at `0.1.0`.
2. CI lints and tests the changed modules (every module when `.go-version`, `.golangci.yml` or `go.work` changes) with `GOWORK=off`, so each builds against its siblings' published versions. It also checks that:
   - every changed module's `VERSION` is a plain `MAJOR.MINOR.PATCH` whose major the module path takes, higher than on `main` and not already tagged;
   - each `VERSION` meets the API-diff floor;
   - `go.work` lists every module, each module has a `README.md`, a `VERSION` and the module path `github.com/sikhlana/pkg/<dir>`, and every go directive names the Go version in `.go-version`.
3. Merge to `main`. CI creates the `<module>/v<version>` tag and a GitHub release for every version not yet released, with notes listing that module's own commits.

Tags are never created by hand. The release tooling lives in [tools/release](./tools/release).

## Adding a module

1. Create the directory with a `go.mod` for `github.com/sikhlana/pkg/<dir>`, a `VERSION` containing `0.1.0`, and a `README.md`.
2. Register it in the root `go.work`.
3. Add a row to the [Modules](#modules) table.
4. Follow the layout and conventions in [openspec/config.yaml](./openspec/config.yaml).

## Working in this repo

Changes are spec-driven with [OpenSpec](https://github.com/Fission-AI/OpenSpec): each module's behavioural guarantees live in `openspec/specs/<module>/spec.md`, and changes are proposed, applied and archived under `openspec/changes/`. [CLAUDE.md](./CLAUDE.md) is the entry point for AI agents.

## License

[MIT](./LICENSE)
