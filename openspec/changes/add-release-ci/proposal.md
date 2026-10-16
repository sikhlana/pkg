## Why

The constitution promises CI-created `<module>/v<version>` tags and a VERSION bump on every module change, but nothing enforces either yet. Without that, the first module can't ship safely. Whether a bump is big enough is also left to judgment, though the API diff can decide most of it.

## What Changes

- New repo tool `tools/release` (Go, run with `go run`), with three commands:
  - `plan`, on pull requests: lists the changed modules (or all of them, when root config changed) so lint and test can start straight away.
  - `check`, on pull requests: every module whose files changed has a valid VERSION, greater than the base branch's, whose tag isn't taken, and at least the minimum gorelease computes from the exported-API diff against the module's last tag. It also checks repo-wide invariants: go.work lists exactly the library modules on disk; each module has README.md and VERSION; every go directive matches `.go-version`.
  - `publish`, on push to main: for every module whose tag for its VERSION has no release, creates a GitHub release that tags the pushed commit (tags follow the go command's layout, so `logger/v2` at 2.0.0 is `logger/v2.0.0`). Release notes are built from that module's own commits. It then asks proxy.golang.org for the new version. Safe to re-run: a missed or failed run is repaired by the next push that leaves the module's VERSION alone, and a version overtaken by another bump is skipped with a warning.
- New pull-request workflow: lint and test only the changed modules (all modules when root config changes), with `GOWORK=off` so each builds against published siblings. It also runs `release check` and lints and tests `tools/release`. One aggregate job gives branch protection a single required check.
- New release workflow on push to main that runs `release publish`.
- Third-party actions pinned by commit SHA, with Dependabot keeping actions and the tool's dependencies current.
- Constitution, README and CLAUDE.md updated:
  - `tools/` holds repo tooling. It has its own go.mod, sits outside go.work, has no VERSION, and is never released.
  - CI enforces the bump floor.
- Repo settings, applied by hand:
  - squash merge only;
  - branches must be up to date before merging;
  - the aggregate check is required;
  - immutable releases are on.

## Capabilities

### New Capabilities

- `release-ci`: what pull-request checks reject and what a push to main publishes. That covers version validation, the API-diff bump floor, repo invariants, changed-module selection, and idempotent tag-and-release with module-scoped notes.

### Modified Capabilities

None.

## Impact

- **Library modules:** none exist yet, so no exported API changes and no VERSION bumps. Every future module becomes subject to `release check`.
- **New tool module `tools/release`:** a `main` package (`github.com/sikhlana/pkg/tools/release`), not a library. It has no fx Module, no Config and no runner. It sits outside go.work, has no VERSION, and is never tagged.
- **New dependencies (tool only):**
  - golang.org/x/mod (semver, go.mod/go.work parsing, module path escaping);
  - google/go-github (releases);
  - spf13/cobra (subcommands);
  - sethvargo/go-githubactions (job summary, annotations);
  - golang.org/x/exp/cmd/gorelease, pinned as a `tool` directive;
  - testify and gomock for tests.
- **New files:**
  - `.github/workflows/` (pull-request and release workflows);
  - `.github/dependabot.yaml`;
  - `tools/release/`.
- **Docs:** `openspec/config.yaml`, `README.md` and `CLAUDE.md`.
- **GitHub:** releases are created with the workflow's `GITHUB_TOKEN` (`contents: write` on the release job only). The repo settings above are changed by hand.
