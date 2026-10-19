## Context

See proposal.md for why. The requirements are in specs/release-ci/spec.md. CI today (`.github/workflows/ci.yaml`) has a `plan` job whose release tool picks the modules a pull request changed, `lint` and `test` matrices over them, a `release-check` job, a `tools-release` job, and `CI OK`, which needs all of them and is the one module check the ruleset requires. Every job builds with `GOWORK=off`, and every action is pinned by commit SHA. golangci-lint's version is pinned in a workflow env var that matches the developers' local version.

govulncheck v1.9.0 against the repo, before this change: every module on main is clean. `telemetry` (PR #22) had four reachable `golang.org/x/net` advisories, fixed there by raising x/net to v0.60.0.

## Goals / Non-Goals

**Goals:**

- A pull request can't merge with a reachable known vulnerability in a module it changes.
- A new advisory against a module nobody is editing is noticed within a week, in one place.
- The same scanner version locally and in CI.

**Non-Goals:**

- Blocking pull requests on advisories in modules they don't touch. The weekly issue covers those.
- Failing on vulnerabilities in dependencies the module's code never calls. govulncheck's default call-graph mode reports only reachable ones, and that's the mode used.
- SARIF or code-scanning uploads. The repo uses neither.
- Dependabot or Renovate. Raising a dependency stays a deliberate edit with its VERSION bump.

## Decisions

### 1. Pull requests scan the modules the plan selects

A `vulncheck` matrix job beside `lint` and `test`, over the same `plan` output, with the same `GOWORK=off` setup, runs `govulncheck ./...` in each module. A change to root configuration already selects every module, so a `.go-version` bump, which can fix or introduce standard-library advisories, scans everything. The `tools-release` job gains a govulncheck step after its tests, since that job runs on every pull request. `CI OK` needs the new job, so a finding fails the required check. A skipped matrix still counts as passing.

**Alternatives:** every module on every pull request (a new advisory for an untouched module would block unrelated work until someone fixed it); a step inside the `test` job (one failure would hide the other, and the job name wouldn't say which failed).

### 2. govulncheck is pinned and installed with `go install`

> Revised in code review: govulncheck is now pinned by a `tool` directive in `tools/release/go.mod`; see [Fixes from code review](#fixes-from-code-review).

A `GOVULNCHECK_VERSION` env var (`v1.9.0`) sits next to `GOLANGCI_LINT_VERSION`, and each job runs `go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}` after `setup-go`. The go command verifies the module through the checksum database, and setup-go is already in every job.

**Alternatives:** `golang/govulncheck-action`, pinned by SHA (it sets Go up itself, duplicating `setup-go`, and its version input would be one more place to keep in step); a `tool` directive in `tools/release/go.mod`, as gorelease has (one pin in go.sum, but every module's job would then build the tool's module to get it).

### 3. The weekly scan is a release-tool subcommand that files one issue

`.github/workflows/vulncheck.yaml` runs on a weekly cron (Monday 06:17 UTC, off the hour) and on `workflow_dispatch`. It's one job: it gets the release tool through the existing `release-tool` action, installs the pinned govulncheck, and runs `release vulncheck --govulncheck <path>`.

The subcommand lives in `tools/release`, next to `plan`, `check` and `publish`, because the tool already has the pieces: the workspace package that reads `go.work`, the report package for the step summary, go-github, and mocked GitHub tests. Logic in a workflow's shell couldn't be tested at all before merge, since `workflow_dispatch` only runs workflows already on the default branch. It:

1. Lists the library modules in `go.work` and every tool under `tools/`.
2. Runs govulncheck in each with `GOWORK=off` and `-format json`, and keeps the findings whose trace reaches a function in the module's code, which is what govulncheck's default text mode reports and what the pull request job fails on. A scan that exits non-zero is an error.
3. Writes a markdown report, one section per affected module listing each advisory's ID, summary, affected module, fixed version and a link, to the step summary.
4. Reconciles the issue through a small interface (find the open issue with the label, create, edit, comment, close), implemented with go-github and mocked in tests:

| Outcome | Issue |
|---|---|
| Any module's scan errors | Untouched; the command fails |
| Findings, no open issue labelled `govulncheck` | Opened with the report |
| Findings, an open issue | Body replaced with the report; a comment when the advisories per module changed since the last report |
| No findings, an open issue | Closed with a comment linking the run |
| No findings, no open issue | Nothing |

The previous findings are read from a hidden marker in the issue body (`<!-- govulncheck-findings: kernel:GO-…,log:GO-… -->`; first written with advisory IDs alone, revised in the second review), so the comparison needs no other state. A replaced body sends no notification and a comment does, so a still-unfixed finding doesn't ping anyone every Monday, while a new advisory does. The label is created when it's missing. The job gets `issues: write`; the workflow stays `contents: read` otherwise. The command succeeds when it found vulnerabilities and filed them: the issue is the alert, and a red scheduled run nobody watches adds nothing.

**Alternatives:** shell with `gh` and `jq` in the workflow (untestable before merge); a Python script (a second language in repo tooling); one issue per advisory (more noise, and closing each needs matching); failing the scheduled run (GitHub mails only whoever last edited the cron); `golang.org/x/vuln/scan` as a library (it would pin a second govulncheck version in the tool's go.mod, apart from the one the pull request job uses).

### 4. The constitution's chain gains govulncheck

The verification chain becomes modernize, golangci-lint, the full tests, then `govulncheck ./...`, on each touched module, and the tasks rule names it. The tooling list adds govulncheck, run directly like the rest. CLAUDE.md's command blocks follow, including the `tools/release` one with `GOWORK=off`.

### 5. The retracted mimetype in two config modules: moved out

`config/configvalidator` and `config/configloader` require `github.com/gabriel-vasile/mimetype` v1.4.15 indirectly, through go-playground/validator, and its author retracted it, so gorelease would fail the next pull request to touch either. This change first raised it to v1.4.16 with patch bumps. Before opening the pull request it moved to its own build pull request: a retraction isn't a vulnerability, govulncheck doesn't report it, and the scan has nothing to do with it.

### Rollup into docs/release.md at archive

Why pull requests scan only their changed modules while a weekly run covers the rest, why findings go to one issue with comments only on change, and why the scheduled run doesn't fail on findings.

## Risks / Trade-offs

- [A new advisory against a dependency of every module lands on a Monday and blocks every pull request that week] → Only modules a pull request changes are scanned, and the fix (raising the dependency) is itself a small pull request. The weekly issue says what to raise.
- [govulncheck reports a vulnerability with no fixed version yet] → The job fails with nothing to raise. Accepted: it's rare, and whoever hits it can decide case by case. No ignore list until one is needed.
- [The vulnerability database is unreachable] → govulncheck errors, the job fails, and a re-run fixes it. The scheduled run leaves the issue alone on errors.
- [The issue reconciliation misbehaves on GitHub in a way the mocks don't show] → Unit tests cover every row of the table against a mocked tracker; after merge, a `workflow_dispatch` run checks the real thing, and a wrong issue is easy to close by hand.
- [govulncheck's JSON stream changes shape] → The tool decodes only the fields it uses (an OSV's ID, summary and affected ranges, a finding's OSV ID, fixed version and trace), with a test fixture recorded from the pinned version.

## Migration Plan

One pull request, in either order with #22: CI runs a pull request's merge with main, so whichever lands second runs the scan over telemetry, which #22 already fixed. After merge, a `workflow_dispatch` run of the weekly workflow on main checks the issue handling for real; with every module clean it should leave the issue tracker alone, which exercises only the no-findings row. The change stays active until that run has been checked, and is archived in a follow-up pull request. Merging publishes no module release. The `govulncheck` label is created by the first run that finds something.

## Fixes from code review

**govulncheck is pinned by a `tool` directive (supersedes decision 2).** The env var had to be kept the same in `ci.yaml` and `vulncheck.yaml`, and the `go install` step was copied three times, with nothing to stop the pull request and weekly scans drifting apart. `tools/release/go.mod` now pins `golang.org/x/vuln/cmd/govulncheck` v1.9.0 next to gorelease. The release-tool action builds and caches it with the tool and exposes a `govulncheck` output. The pull request matrix job and the weekly job both take the binary from there, and the `tools-release` job runs `go tool govulncheck` in its own module. Locally, `go install golang.org/x/vuln/cmd/govulncheck` from `tools/release` installs the same version. The repo's constitution keeps running toolchains directly rather than in Docker, as the user confirmed against their global rule, and now records where govulncheck's version comes from.

**Reconciliation is ordered so a half-failed run repeats a step instead of skipping one.** A changed set of IDs is commented on before the body, and its marker, is replaced; a failed comment used to leave the new IDs recorded and the change never announced. Closing happens before the closing comment; a failed close used to repeat the comment every week.

**Other fixes:**

- An issue body is capped at `MaxBody` (60,000 bytes, under GitHub's 65,536-character limit). A longer report becomes a short form listing each affected module's advisory IDs, pointing at the run's step summary, and still ending with the full marker.
- `GitHub.Find` pages through every labelled open item rather than reading only the first 100, so labelled pull requests can't hide the issue and cause a duplicate.
- `ParseMarker` reads the last marker in the body, so an older report or note pasted above it doesn't skew the comparison.
- A run without `--dry-run` fails on an empty `GITHUB_TOKEN` before scanning, not after minutes of it.
- `vulncheck.Options` takes the `Workspace` and derives the root and the directories from it, instead of a root and a directory list that had to agree.
- `Govulncheck.Run` resolves its path to an absolute one before running in a module's directory, so a relative path works; `main` only checks the binary exists up front.

## Fixes from the second code review

- **Changes are detected per module.** The marker holds module:ID pairs instead of advisory IDs, so an advisory already listed for one module that becomes reachable from another is a change and is commented on ("New: GO-… in kernel"). The spec's requirement now says "advisories found in each module".
- **When the list would be too long, the marker holds a SHA-256 digest of the findings.** Per-module pairs can outgrow an issue body where deduplicated IDs couldn't. A digest still detects a change, but the comment then can't say what changed and points at the body instead. Found by the size test while making the previous fix.
- **A missing marker means no history**, so the body is replaced without a comment rather than announcing every advisory as new after a hand edit.
- **`NewGovulncheck` resolves the binary once**, to an absolute path, so `main`'s pre-flight check and the per-module lookups became one; the `Govulncheck` path field is unexported.
- **The short body's size check counts the newline before the marker**, so `Body` can't return `MaxBody`+1 bytes.
- **The pull request scan restores the module's dependency cache** with its own `setup-go`, as the test job does, instead of running Go set up by the release-tool action without a cache.
- A stale doc comment for the removed `lookPathAbs` no longer merges into `newTracker`'s, and the action's cache-key comment is rewrapped.

## Fixes from the third code review

Run at a higher effort, it found fifteen issues; fourteen were fixed, and one (parallel scans) was declined.

- **A run dispatched on a branch other than the default is a dry run.** The issue tracks the default branch, and a dispatch on a branch trying a fix would otherwise have closed it while main was still affected.
- **The tools/release scans add the `tool` pattern**, so gorelease and govulncheck, built from its go.mod and run on pull request content, are scanned too. In modules without tool directives the pattern only warns, so the weekly scan passes it everywhere.
- **A fix landing is a change.** A marker entry carries `+fix` when its advisory has a fixed version, and the comment gains "Fix available" (with the version) and "Fix no longer listed" sections.
- **A partial scan still reaches the summary.** `Scan` returns the completed modules' results with an error wrapping the new `ErrIncomplete`; the issue is still left alone, and the summary is marked incomplete.
- **The digest comment points at the run's step summary**, since the cut-down body may not list the change either.
- **Tool discovery moved to `workspace.ToolDirs`**, sharing the go command's skip rules (`.`, `_`, `testdata`) with the module walk through `workspace.Skipped`; a parked `tools/_attic` no longer fails the weekly scan.
- **Repository names are parsed once**, by a new `repository` package that both `publish` and `vulncheck` use, replacing two copies and two `ErrRepository` sentinels with `repository.ErrInvalid`.
- **The release-tool action takes `setup-go: "false"`**, so the pull request scan's own cached `setup-go` isn't followed by a second one restoring another cache over it on a cache miss.
- **govulncheck and gorelease get `cmd.Environ()`**, so `PWD` matches the directory they run in.
- **Dependencies render as `module version`**, since `module@version` was autolinked as an email address.
- **README and CLAUDE.md**: the marker's real name, the close-then-comment order, and the README's row in CLAUDE.md's table naming the weekly scan.
- **Declined: running the scans in parallel.** Ten modules take minutes in a thirty-minute job on a two-core runner where each govulncheck already uses both cores; worth revisiting if the job nears its timeout.

## What actually happened

Merged as #23. Pull requests run govulncheck on their selected modules and on `tools/release` (with the `tool` pattern), and `.github/workflows/vulncheck.yaml` runs `release vulncheck` weekly and on demand, keeping one `govulncheck` issue. Deviations from the decisions above, each recorded in the review sections:

- govulncheck is pinned by a `tool` directive in `tools/release/go.mod` and built by the release-tool action, not by a workflow env var and `go install` (decision 2).
- The marker records module:ID findings with `+fix` (or a digest), so an advisory spreading to another module or gaining a fix comments too, and a hand-edited body is replaced silently (decision 3).
- A dispatch on a branch other than main is a dry run, and a partial scan still reaches the step summary, marked incomplete.
- The mimetype bumps (decision 5) moved to their own pull request, #24, before #23 opened: a retraction isn't a vulnerability.
- Running the scans in parallel, declined in the third review, was done afterwards by the `parallelise-release-tool` change (#25), which supersedes that note.

After #23 merged, the weekly workflow was run by hand on main: it scanned every module, found nothing, and left the issue tracker alone, as expected with every module clean. That exercised the no-findings row against the real API; the rows that open, update and close the issue have so far run only against the mocked tracker.

## Follow-ups left open

- The issue-opening, updating and closing paths will first run for real when an advisory lands on main; check the first issue the workflow opens.
- The pull request vulnerability job first runs on real modules with the next pull request that changes one.
