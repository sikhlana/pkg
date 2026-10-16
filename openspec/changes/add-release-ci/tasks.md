## 1. Tool scaffold

- [x] 1.1 Branch off `origin/main` after `git fetch`.
- [x] 1.2 Create the `tools/release` module (`github.com/sikhlana/pkg/tools/release`).
  - Go version from `.go-version`.
  - Not registered in go.work; no VERSION file.
  - Require golang.org/x/mod, google/go-github, spf13/cobra, sethvargo/go-githubactions, testify and gomock.
  - Pin gorelease with `tool golang.org/x/exp/cmd/gorelease`.
  - Done when `GOWORK=off go build ./...` passes from `tools/release`.
- [x] 1.3 Lay out the code:
  - a thin `main.go` that wires the cobra commands `plan`, `check` and `publish`, with no `init()`;
  - all logic in importable packages, so tests can be external test packages;
  - the repo root found with `git rev-parse --show-toplevel`.

## 2. Repository model

- [x] 2.1 Load library modules from go.work, skipping anything under `tools/`. For each, read its directory, module path, go directive and VERSION. Test against fixture repos built in `t.TempDir()`.
- [x] 2.2 Git helpers:
  - merge base;
  - files changed between two refs;
  - a file's contents at a ref;
  - a module's tags and its highest semver tag;
  - whether a tag exists;
  - commit subjects for a range limited to a path.
  Test against temporary git repos.
- [x] 2.3 Repo-wide checks:
  - go.work matches the go.mod directories outside `tools/` (both missing and stale entries);
  - every module has README.md and VERSION;
  - go.work's and every module's go directive equals `.go-version`.
  Write one test per spec scenario: missing from go.work, stale entry, Go version drift, missing README.

## 3. plan

- [x] 3.1 Select changed modules:
  - diff from the merge base to HEAD;
  - a module is changed when a file under its directory changed;
  - a change to `.go-version`, `.golangci.yml` or `go.work` selects every module.
  Write the selection as a JSON array step output for the matrix, plus whether it's empty.
- [x] 3.2 Tests for the spec scenarios: one module changed, root configuration changed, no module changed.

## 4. check

- [x] 4.1 VERSION rules for each module with changed files:
  - present, and exactly MAJOR.MINOR.PATCH;
  - `0.1.0` when the module doesn't exist on the base branch;
  - otherwise greater than the base branch's VERSION;
  - `<dir>/v<VERSION>` not already tagged.
  Write one test per spec scenario.
- [x] 4.2 gorelease runner:
  - build the pinned gorelease once, into a temp directory, from `tools/release` with `GOWORK=off`;
  - run it in each module's directory with `GOWORK=off` and `-base=v<highest tag>`, or `-base=none` for a module with no tags;
  - parse the `Suggested version:` line;
  - fail on a non-zero exit or a missing suggestion, showing the output;
  - otherwise require VERSION to be at least the suggestion.
- [x] 4.3 gorelease tests, skipped under `-short`, using the isolation recipe from the design:
  - `GOENV=off`;
  - a temporary `GIT_CONFIG_GLOBAL` with an `insteadOf` rule;
  - `GOPRIVATE`, `GONOSUMDB` and `GONOPROXY` for the module path;
  - `GIT_ALLOW_PROTOCOL=file`.
  Cover: no API change, new export with a patch bump, breaking change before 1.0, breaking change after 1.0, unpublished sibling requirement, failed comparison.
- [x] 4.4 Reporting:
  - run the repo-wide checks on every pull request;
  - write the job summary: each changed module's base version, new version and outcome, plus its gorelease report;
  - attach each error to the file to fix (VERSION, go.mod, go.work);
  - exit non-zero on any failure.
  Test the spec's "mixed results" scenario.

## 5. publish

- [x] 5.1 A small GitHub client interface (look up a release by tag, create a release), implemented with go-github from `GITHUB_TOKEN` and `GITHUB_REPOSITORY`. Generate its mock with mockgen from a `//go:generate` directive.
- [x] 5.2 Create missing releases. For each library module, if `<dir>/v<VERSION>` has no release, create one:
  - tag name and title are the tag; the target is the pushed SHA; `make_latest=false`;
  - an existing tag keeps its commit.
  Test with the mock: one module bumped, several bumped, re-run, missed release repaired, tag without a release.
- [x] 5.3 Release notes: subjects of commits touching the module's directory since its previous release tag, or every such commit for a first release. Test that notes exclude other modules.
- [x] 5.4 Announce each new version to the module proxy:
  - GET `<proxy>/<escaped module path>/@v/v<version>.info`, escaping the path with `module.EscapePath`;
  - time out after a short limit;
  - on failure, report a warning, not an error.
  Test against an `httptest` server.
- [x] 5.5 With zero library modules, `publish` succeeds without calling GitHub.

## 6. Workflows

- [x] 6.1 `.github/workflows/ci.yaml` on pull requests:
  - read-only permissions by default; `actions/checkout` with `fetch-depth: 0`; `setup-go` reading `.go-version`, with `GOTOOLCHAIN=local`;
  - `plan`, feeding lint and test matrices that run with `GOWORK=off` and are skipped when the plan is empty;
  - golangci-lint pinned to the version run locally;
  - `release-check`, with `--base` set to the PR's base branch;
  - `tools-release` (lint and test with `GOWORK=off`);
  - `ci-ok`, which needs every other job, runs `if: always()`, and fails only on failed or cancelled jobs.
- [x] 6.2 `.github/workflows/release.yaml` on push to main:
  - `contents: write`; `concurrency: release` with `cancel-in-progress: false`; `fetch-depth: 0`;
  - runs `publish` with the pushed SHA.
- [x] 6.3 Pin every third-party action by commit SHA, with a version comment.
- [x] 6.4 `.github/dependabot.yaml`: `github-actions` at `/`, and `gomod` at `/tools/release`.
- [x] 6.5 Lint both workflows with actionlint.
- [x] 6.6 Cache the built tool instead of compiling it in every job:
  - a local composite action, `.github/actions/release-tool`, restores the tool and its pinned gorelease from the Actions cache, or builds and saves both on a miss;
  - the key hashes `.go-version`, `tools/release/go.mod` and `go.sum`, and the tool's non-test Go files;
  - on a hit, `plan` and `publish` skip `setup-go`; `release-check` sets up Go without the dependency cache;
  - `check` takes `--gorelease <path>` for the prebuilt binary.

## 7. Docs

- [x] 7.1 Constitution (`openspec/config.yaml`):
  - add the `tools/` rule: own go.mod, outside go.work, run with `GOWORK=off`, no VERSION, never released; code standards apply, module README and spec rules don't;
  - replace "edits before its first tag don't bump" with "a new module starts at 0.1.0 and merging it publishes it";
  - note that CI enforces the API-diff minimum.
- [x] 7.2 `tools/release/README.md`: what `plan`, `check` and `publish` do, and how to run `check` locally against `origin/main`.
- [x] 7.3 `CLAUDE.md`: add the `tools/release` test and lint commands, which run with `GOWORK=off`, to Build and test.
- [x] 7.4 `README.md`: under Versioning and releases, add the PR checks and the API-diff minimum.
- [x] 7.5 Run `openspec doctor` and `openspec validate add-release-ci --strict`.

## 8. Verification

No library module is touched, so there are no VERSION bump or module README tasks. `tools/release` has no VERSION.

- [x] 8.1 In `tools/release`, run `GOWORK=off go generate ./...` and check the generated mock is current.
- [x] 8.2 In `tools/release`, with `GOWORK=off`:
  - `modernize -fix ./...`;
  - `golangci-lint run --fix ./...`, and again without `--fix` if it changed files;
  - the full `go test ./...`.
- [x] 8.3 Run `go test -short ./...` across every module in go.work (none yet, so confirm the loop runs cleanly), then in `tools/release` with `GOWORK=off`. If anything fails, fix it and repeat 8.2 and 8.3.
- [x] 8.4 Run `plan` and `check` locally against `origin/main`: both report no modules and pass.

## 9. Code review fixes

- [x] 9.1 Tags and majors:
  - `workspace.Module` derives Go's tag prefix (a `<dir>/vN` directory is dropped);
  - it filters releases by the module path's major;
  - it gives the first version: `0.1.0`, or `N.0.0` for `/vN`.
- [x] 9.2 `check` and `publish` reject a VERSION whose major the module path doesn't take.
- [x] 9.3 New repo invariant: the module path is the repository path (from `tools/release/go.mod`) plus the directory. Go directives are compared by language version.
- [x] 9.4 `check`: a module absent on base with releases is restored and continues above its last release. Once VERSION fails, the tag check and gorelease are skipped.
- [x] 9.5 `plan` judges ownership against both the base and PR layouts, covering nested-module carve-outs and deletions.
- [x] 9.6 `publish`:
  - tags HEAD, refuses a dirty tree, and drops `--sha`;
  - warns about versions skipped since the previous release.
- [x] 9.7 gorelease timeout and `WaitDelay`; `timeout-minutes` on every job; the action's own file in its cache key; Dependabot `allow: all` for gorelease's indirect pin.
- [x] 9.8 Cleanups: `report.Cell` shared by `check` and `publish`; `version.Below` replaces `Highest`; gorelease's temp directory is created only when built.
- [x] 9.9 Spec, design, proposal, constitution and READMEs updated to match.
- [x] 9.10 Re-ran 8.1 to 8.4: mock unchanged, `modernize -fix`, golangci-lint clean with and without `--fix`, the full `go test ./...` (gorelease integration included), the short test run, and `plan` and `check` against `origin/main`.

## 10. Rollout

- [ ] 10.1 Open the PR. `ci.yaml` runs, and the CI OK check (job `ci-ok`) passes with no library modules.
- [ ] 10.2 After merge, `release.yaml` runs, and `publish` creates nothing.
- [ ] 10.3 You apply the repo settings by hand:
  - squash merge only;
  - a rule on main requiring the CI OK check (job `ci-ok`) and up-to-date branches;
  - immutable releases on.
