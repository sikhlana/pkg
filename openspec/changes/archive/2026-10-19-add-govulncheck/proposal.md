## Why

Nothing in CI looks for known vulnerabilities in a module's dependencies. While `telemetry` was in review, a local `govulncheck` run found four reachable vulnerabilities in `golang.org/x/net` that lint, tests and gorelease had all passed over. And advisories appear without any code change, so a module nobody edits can become vulnerable with no pull request to notice.

## What Changes

- **Pull requests run govulncheck** on every module CI selects for lint and test, and on `tools/release`. A vulnerability reachable from the module's code fails the job, and the pull request's overall status with it.
- **A weekly workflow scans every module** in `go.work` and every tool under `tools/`, and can be started by hand. Its findings go to one issue labelled `govulncheck`: opened when there are findings and no such issue is open, its body replaced with each run's report, a comment added when the set of advisories changes, and closed with a comment when a run finds nothing. A scan that errors fails the run without touching the issue. The logic is a new `vulncheck` subcommand of the release tool, so it's unit-tested like `publish`.
- **govulncheck is pinned** by a `tool` directive in `tools/release/go.mod`, as gorelease is, and built by the release-tool action for every job that scans, so pull requests, the weekly run and developers use the same scanner.
- Constitution: govulncheck joins the tooling list and the verification chain, after the full tests, and the task rule for verification names it. CLAUDE.md's build-and-test commands follow.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-ci`: pull requests scan their selected modules and `tools/release` for reachable vulnerabilities, and a weekly scan of every module reports its findings in a single issue.

## Impact

- **`tools/release`:** a `vulncheck` subcommand with an issue-tracker interface, its go-github implementation and a mock, and a `tool` directive pinning govulncheck. It's repo tooling with no VERSION.
- **No library module changes.** `config/configvalidator` and `config/configloader`'s retracted mimetype (a gorelease failure, not a vulnerability) moved to its own pull request. `telemetry`'s own x/net fix went into its pull request (#22).
- **CI:** `.github/workflows/ci.yaml` gains a vulnerability job per selected module and a govulncheck step in the `tools/release` job, both rolled into `CI OK`. A new `.github/workflows/vulncheck.yaml` runs weekly and on demand, with `issues: write`.
- **New dependency:** `golang.org/x/vuln/cmd/govulncheck`, a tool directive in `tools/release/go.mod` only; no library module requires it.
- **Docs and repo files:** `openspec/config.yaml` (tooling, verification chain, tasks rule), `CLAUDE.md`, `docs/release.md` (what pull request CI runs, and the weekly scan).
- **Release:** merging publishes nothing; no library module's files change.
