# fx-modules

A personal library of reusable Go modules, wired through [Uber fx](https://uber-go.github.io/fx/). Each subdirectory with a `go.mod` is its own module, versioned and tagged on its own (`<module>/v<version>`). There's no root `go.mod`; the committed `go.work` ties the modules together locally.

## Read these first

| File                            | Owns                                                                                                               |
|---------------------------------|--------------------------------------------------------------------------------------------------------------------|
| openspec/config.yaml            | The constitution: fx patterns, config vs wiring, code and test standards, tooling, versioning, docs and spec rules |
| .golangci.yml                   | Lint and format rules, including the bans the constitution calls for                                               |
| README.md                       | The human entry point: module table, install, release flow, adding a module                                        |
| <module>/README.md              | One module's Configuration, Exported Members and Usage                                                             |
| openspec/specs/<module>/spec.md | The behavioural guarantees a module makes to consumers                                                             |
| docs/<topic>.md                 | Why a cross-module pattern or trade-off is shaped the way it is                                                    |

## Making a change

Changes go through OpenSpec: `/opsx:propose`, then `/opsx:apply`, `/opsx:verify` and `/opsx:archive`. Use `/opsx:explore` to think something through first. OpenSpec injects the constitution into every artifact, and its `rules` block says what each proposal, design, task list and spec must contain.

## Build and test

There's no root `go.mod`, so `go test ./...` fails at the repo root; run Go commands from a module's directory. A change is done when the constitution's verification chain passes. On each module you touched:

```bash
go generate ./...              # only if a mocked interface changed
modernize -fix ./...
golangci-lint run --fix ./...  # re-run without --fix if it changed files
go test ./...
```

Then the short sweep across every module in `go.work`:

```bash
go list -m -f '{{.Dir}}' | while IFS= read -r mod; do (cd "$mod" && go test -short ./...); done
```
