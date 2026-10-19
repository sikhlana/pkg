## 1. Dependencies

- [x] 1.1 Branch `add-govulncheck` off `origin/main` after `git fetch`.

Tasks 1.2–1.4 (mimetype v1.4.16 and patch bumps in `config/configvalidator` and `config/configloader`) moved to their own pull request; see design decision 5.

## 2. Pull request scan

- [x] 2.1 In `.github/workflows/ci.yaml`, add `GOVULNCHECK_VERSION: v1.9.0` beside `GOLANGCI_LINT_VERSION`, with the same "the version developers run locally" note.
- [x] 2.2 Add a `vulncheck` job, "Vulnerabilities (<module>)", over the `plan` matrix with `GOWORK: "off"`: check out, set up Go from `.go-version`, `go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}`, then `govulncheck ./...` in the module. Same timeout shape as `test`.
- [x] 2.3 In the `tools-release` job, install govulncheck the same way and run it in `tools/release` after the tests.
- [x] 2.4 Add `vulncheck` to `CI OK`'s `needs`, and update the comment above it if it lists the jobs.

## 3. release vulncheck

- [x] 3.1 A `vulncheck` package: decode govulncheck's `-format json` stream into the fields design decision 3 names, and keep the findings whose trace reaches a function in the module. Record a fixture from govulncheck v1.9.0 with one reachable and one unreachable finding for the tests.
- [x] 3.2 Scanning: list the library modules from the workspace and every tool directory under `tools/`, run the given govulncheck binary in each with `GOWORK=off`, and return the findings per module, failing on a non-zero exit. The runner is behind a function or interface so tests don't need govulncheck.
- [x] 3.3 The report: markdown with one section per affected module and each advisory's ID, summary, affected dependency, fixed version and link, plus the hidden `govulncheck-ids` marker. A function that reads the marker back.
- [x] 3.4 The tracker: an interface for finding the open issue with a label, creating, editing, commenting, closing and ensuring the label exists, with a `//go:generate` mockgen directive, and a go-github implementation like `publish.GitHub`.
- [x] 3.5 Reconciliation implementing the table in design decision 3, with the run's URL (from `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY` and `GITHUB_RUN_ID`) in the closing comment.
- [x] 3.6 `newVulncheckCommand` in `main.go`: `release vulncheck --govulncheck <path>`, defaulting to `govulncheck` on `PATH`, writing the report to the step summary and reconciling the issue; `--dry-run` writes the report and skips the tracker, needing no token. Register it on the root command.
- [x] 3.7 Tests for every row of the reconciliation table against the mock, the marker round trip, the JSON filtering against the fixture, and a scan error leaving the tracker untouched.
- [x] 3.8 Run `GOWORK=off go generate ./...` in `tools/release`.

## 4. Weekly workflow

- [x] 4.1 `.github/workflows/vulncheck.yaml`: `schedule` (`17 6 * * 1`) and `workflow_dispatch`; `permissions: contents: read` at the top and `issues: write` on the job; check out, get the release tool through `./.github/actions/release-tool` with Go set up, install the pinned govulncheck, run `release vulncheck`. Pin every action by SHA, as `ci.yaml` does, and give the job a timeout.
- [x] 4.2 Keep `GOVULNCHECK_VERSION` in one place: the weekly workflow reads the same value as `ci.yaml` (a shared env file the jobs source, or the value duplicated with a comment pointing at the other, whichever the release tool's action already does for gorelease).

## 5. Constitution and docs

- [x] 5.1 In `openspec/config.yaml`: add govulncheck to the Tooling list; add `govulncheck ./...` after the full tests in the verification chain; add it to the tasks rule's verification task.
- [x] 5.2 In `CLAUDE.md`, add `govulncheck ./...` after `go test ./...` in both command blocks (`GOWORK=off` in the `tools/release` one).
- [x] 5.3 In `docs/release.md`, add the vulnerability job to what pull request CI runs, and a section on the weekly scan from the rollup list in design.md.
- [x] 5.4 In `tools/release/README.md`, document `vulncheck` next to `plan`, `check` and `publish`.

## 6. Verification

Tasks 6.1–6.2 (verifying the two config modules) moved with the mimetype bumps.

- [x] 6.3 The same chain in `tools/release/` with `GOWORK=off`.
- [x] 6.4 Run `release vulncheck --dry-run` locally against the real modules, confirming it scans every library module and tool and reports none affected.
- [x] 6.5 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat both verification steps.

## 7. Code review fixes

See design.md's Fixes from code review.

- [x] 7.1 Pin govulncheck with a `tool` directive in `tools/release/go.mod`; build it in the release-tool action with a `govulncheck` output; use that output in the pull request matrix and weekly jobs, and `go tool govulncheck` in the `tools-release` job; drop `GOVULNCHECK_VERSION` and the `go install` steps.
- [x] 7.2 Comment before replacing the body when the IDs change, and close before commenting when there are no findings.
- [x] 7.3 Cap issue bodies at `MaxBody` with a short form that keeps the marker.
- [x] 7.4 Page through `Find`'s results.
- [x] 7.5 Read the last marker in `ParseMarker`.
- [x] 7.6 Fail on an empty `GITHUB_TOKEN` before scanning.
- [x] 7.7 Pass the `Workspace` in `vulncheck.Options`; resolve govulncheck's path inside `Govulncheck.Run`.
- [x] 7.8 Tests for each fix; README, `docs/release.md`, CLAUDE.md and the constitution's tooling note updated.
- [x] 7.9 Re-run section 6.

## 8. Second code review fixes

See design.md's Fixes from the second code review.

- [x] 8.1 Record module:ID findings in the marker, comparing and describing changes per module, with a SHA-256 digest when the list would be too long.
- [x] 8.2 Replace a body without a marker without commenting.
- [x] 8.3 Add `NewGovulncheck`, resolving the binary once; use it in `main`.
- [x] 8.4 Count the newline before the marker in `Body`'s size check.
- [x] 8.5 Give the pull request scan its own `setup-go` with the module's dependency cache.
- [x] 8.6 Remove the stale `lookPathAbs` comment; rewrap the action's cache-key comment.
- [x] 8.7 Tests for each fix; spec, README and `docs/release.md` updated.
- [x] 8.8 Re-run section 6.

## 9. Third code review fixes

See design.md's Fixes from the third code review.

- [x] 9.1 Make a dispatch on a non-default branch a dry run in `vulncheck.yaml`.
- [x] 9.2 Scan with the `tool` pattern in the `tools-release` job and in `Govulncheck.Run`.
- [x] 9.3 Mark findings with a fix in the marker; comment on fixes landing and being withdrawn.
- [x] 9.4 Return partial results with `ErrIncomplete` and mark the summary incomplete.
- [x] 9.5 Point the digest comment at the step summary.
- [x] 9.6 Add `workspace.Skipped` and `workspace.ToolDirs`; use them in vulncheck.
- [x] 9.7 Add the `repository` package and use it in `publish` and `vulncheck`.
- [x] 9.8 Add the release-tool action's `setup-go` input and use it in the pull request scan.
- [x] 9.9 Use `cmd.Environ()` in govulncheck and gorelease runs; render dependencies as `module version`.
- [x] 9.10 Fix the README's marker name and close order, and CLAUDE.md's README row.
- [x] 9.11 Tests for each fix; spec and `docs/release.md` updated.
- [x] 9.12 Re-run section 6.
