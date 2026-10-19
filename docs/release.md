# Release CI

[tools/release/README.md](../tools/release/README.md) says what the release CI enforces and how to run it. This page says why it's shaped that way. The full discussion, including the alternatives rejected, is in the archived [add-release-ci](../openspec/changes/archive/2026-10-16-add-release-ci/design.md) change.

## You set the version; the API diff sets a minimum

The PR author bumps `VERSION`, and CI fails the PR if it's lower than the lowest version the exported-API change allows. The author can always go higher: a behaviour change behind an unchanged API still deserves a minor, and no tool can see that.

This means the version is decided in the PR that makes the change, where the reviewer can see both together. The alternatives each lost something:

- **Commit-message tools** (release-please and the like) move the bump into a separate bot PR, add a PR to every release, and need a token beyond the default `GITHUB_TOKEN` for CI to run on the bot's PRs.
- **Versions computed entirely from the API diff** can't tell a behaviour change from a refactor.

## The minimum comes from gorelease's suggestion, not its exit status

gorelease compares a module against a released version and suggests the next one. Its exit status is no use before 1.0: given `-version`, it accepts a patch bump for new exports and for breaking changes alike. So `check` runs it without `-version`, reads the `Suggested version:` line, and compares that with `VERSION` itself. Past 1.0, a breaking change produces no suggestion at all, and `check` reports that it needs a new major module path.

A spike against Go 1.27 confirmed the suggestions match the constitution's bump rules, dependency updates included (a patch). It also showed:

- **No releases means `-base=none`,** or gorelease errors out.
- **The base is passed explicitly,** taken from git tags. gorelease's own lookup goes through the module proxy, which can lag a tag pushed minutes earlier.
- **Requirements on unpublished siblings fail.** gorelease builds the module outside the workspace, so it catches a requirement on a sibling version that hasn't been published, the rule that keeps `go.work` from hiding a broken release.
- **`replace` directives aren't flagged.** golangci-lint's `gomoddirectives` covers those.

gorelease is experimental and still runs a deprecated `go get -d`. It's pinned by a `tool` directive in the tool's `go.mod`, given a timeout, and allowed to block: when it breaks, CI fails visibly rather than letting an unchecked version through.

## Publishing creates the releases that should exist

`publish` doesn't look at what a push changed. For every module, it checks whether the current `VERSION` has a release and creates it if not, on HEAD. Re-running is harmless, and a failed run is repaired by the next push that leaves the module's `VERSION` alone.

That's only correct because of the bump rule: a module's files never change without its `VERSION` changing. Every commit with the same `VERSION` therefore has the same module contents, and it doesn't matter which one gets the tag.

Two gaps are accepted:

- **Skipped versions.** If a publish fails and the next merge bumps the module again, the missed version is never released. Go handles version gaps, and the later release contains everything. `publish` reads `VERSION`'s history and warns about each skipped version, so the gap is never silent. Tagging old commits would mean releasing versions nobody reviewed as a release.
- **LICENSE.** The go command copies the root `LICENSE` into every module's zip, but editing it doesn't select any module. A repair could therefore tag a later commit whose `LICENSE` differs. Consumers only ever see the tagged commit's zip, which is immutable. Making every module bump for a `LICENSE` edit would cost far more.

`publish` reads `VERSION` files from the working tree and tags HEAD, so it refuses to run on a dirty tree. There's deliberately no way to tag another commit: a commit flag that didn't match the checkout would tag one commit with another's version.

Immutable releases are on. A published release's tag can't be moved or deleted, which matches the module proxy, where a version is fixed once fetched. Mistakes are fixed forward with a new patch version.

After creating a release, `publish` requests the new version from proxy.golang.org, so `go get <module>@v<version>` works straight away and pkg.go.dev picks it up. A failed request is only a warning: the tag is already public, and the proxy fetches it from GitHub on its first request anyway. The request doesn't refresh the proxy's `@latest` answer, which catches up on its own schedule. After `cli/v0.2.0` was published, `@latest` answered v0.1.0 for a while.

## "Latest" is GitHub's call

A repository of independently versioned modules has no single latest release, so `publish` never asks GitHub to mark one. That doesn't stop GitHub: when no release is marked latest, it treats the newest full release as latest. Publishing `cli/v0.1.0` and then `cli/v0.2.0` showed the badge moving to each in turn. Leaving the flag out of the request would get the same result, so `publish` keeps sending `make_latest: false` rather than claim a choice it doesn't make. Marking releases as prereleases would hide the badge, but would mislabel stable releases.

## Commit types describe; VERSION decides

Commit subjects and pull request titles follow Conventional Commits, and squash merges make the pull request title `main`'s commit subject. Since `publish` builds release notes from those subjects, the notes say what kind of change each line is, with no change to `publish`.

The type never sets a version. A `feat(cli)` pull request that adds exports still bumps `cli/VERSION` by hand, and the gorelease floor still checks it. This is the commit-message versioning rejected above, kept out on purpose: a `fix` can be a behaviour change worth a minor, and only the author can tell. The PR title check (`pr-title.yaml`) checks the form, not the bump.

## Tags and majors follow the go command

The go command looks for `logger/v2`'s versions under `logger/`, not `logger/v2/`. So a module in a major version directory is tagged without it: `logger/v2` at 2.1.0 is `logger/v2.1.0`.

`logger` and `logger/v2` then share a tag prefix. Each module's releases are only the tags whose major its module path takes: v0 and v1 for `logger`, v2 for `logger/v2`. Without that filter, `logger` would see `logger/v2.0.0` as its latest release, and every later comparison against it would fail.

The same rule guards `VERSION`. A v2 on a path without `/v2` would produce a tag the go command rejects, and immutable releases would make it permanent. `check` rejects it, and `publish` refuses it too.

Every module's path must be the repository path followed by its directory. That's what makes the go command look for its tags where they are. It also means a new major lives in its own `<dir>/vN` directory rather than replacing the old one in place, so the old major's line stays maintainable on `main`.

## New, restored and existing modules

What a module's `VERSION` must exceed depends on two facts: whether it exists on the base branch, and whether it has releases.

- **New** (neither): it starts at its path's first version, `0.1.0`, or `N.0.0` for a `/vN` path. A fixed `0.1.0` would make the `/v2` move impossible.
- **Restored** (released once, then removed): it continues above its last release. Starting again at `0.1.0` would collide with its old tags.
- **Existing:** above its `VERSION` on the base branch.

"Has it been tagged?" was the wrong test. A module that merged at 0.1.0 but whose publish failed would be stuck: the next PR would need a version both above 0.1.0 and equal to it.

## Which modules a pull request changes

CI lints, tests and scans for vulnerabilities only the modules a PR changes, and that's enough because it builds with `GOWORK=off`. Each module sees its siblings' published versions, so a PR can't break an unchanged module in CI. The local `go.work` sweep remains the early warning for siblings that will upgrade later. A change to root config (`.go-version`, `.golangci.yml`, `go.work`) selects every module for lint, test and scan, without requiring bumps. A `.go-version` bump can fix or introduce standard-library advisories, so scanning everything then is the point.

"Changed" means the module's contents changed, judged against both the base layout and the PR's:

- an added file counts for its owner at HEAD;
- a deleted file counts for its owner at the merge base;
- a modified file counts for both;
- an unchanged file whose owner moved counts for both owners.

Judging by HEAD's layout alone gets two cases wrong:

- **Moving a package into its own nested module** (adding `logger/sub/go.mod`) silently drops a package from `logger`, without marking `logger`.
- **Deleting a nested module outright** marks its parent for no reason.

## Vulnerabilities: changed modules per PR, everything weekly

Each module CI selects also gets a govulncheck job, and so does `tools/release`, whose scan adds the `tool` pattern: gorelease and govulncheck are built from its `go.mod` and run on pull request content, so their code counts as code CI builds. govulncheck runs in its default call-graph mode, so only a vulnerability the module's code can reach fails the job; one in a dependency function it never calls doesn't. Its version is pinned by a `tool` directive in `tools/release/go.mod`, as gorelease's is, and the release-tool action builds it once for every job that scans, so pull requests and the weekly run can't drift apart. A version in a workflow env var, installed with `go install` in each job, was the first cut; it needed the same value kept in two workflows.

A PR scans only the modules it changes. Scanning every module would let a new advisory against an untouched module block unrelated work until someone fixed it. Advisories also appear without any code change, though, so a module nobody edits can become vulnerable with no PR to notice. The weekly `vulncheck.yaml` covers that: `release vulncheck` scans every library module in `go.work` and every tool under `tools/`, and can be started by hand.

Its findings go to one issue labelled `govulncheck`, not one per advisory: fewer issues, and nothing to match up when closing. Each run replaces the issue's body with its report, which notifies nobody, and comments only when the advisories found in each module changed, which notifies the watchers; an advisory spreading to another module is a new exposure and counts, and so does an advisory getting a fixed version, the moment it becomes something to act on. A still-unfixed advisory doesn't ping anyone every Monday; a new one does. The previous findings, as module:ID pairs, live in a hidden marker in the body, so the comparison needs no other state; when listing them would crowd the body, the marker holds their SHA-256 digest instead, which still tells a change apart but can't say what changed. A body someone edited by hand, with no marker left, is replaced without a comment, since there's no history to compare. A run that finds nothing closes the issue with a comment linking the run.

Each case does first the step whose loss would go unnoticed, so a run that fails halfway repeats a step next time instead of skipping one. A change is commented on before the body (and its marker) is replaced; otherwise a failed comment would leave the new IDs recorded and the change never announced. The issue is closed before the closing comment; otherwise a failed close would repeat the comment every week. A report too long for an issue body (GitHub caps it at 65,536 characters) is cut down to the advisory IDs per module, pointing at the run's step summary for the rest.

The issue tracks the default branch. A run dispatched by hand on another branch scans and reports in its summary but leaves the issue alone; otherwise trying a fix on a branch would close the issue while main is still affected.

The scheduled run succeeds when it finds vulnerabilities and files them. The issue is the alert. A red scheduled run mails only whoever last edited the cron, so failing on findings would add noise without reaching anyone. A scan that can't run (the vulnerability database unreachable, say) does fail the run, and leaves the issue alone, since a partial scan would read as fixed advisories. The summary still shows what the other modules' scans found, marked incomplete, so one flaky module doesn't hide the rest.

The logic is a release tool subcommand rather than shell in the workflow because `workflow_dispatch` only runs workflows already on the default branch: shell couldn't be tried before merge, while the subcommand is unit-tested against a mocked tracker. It shells out to the pinned govulncheck binary rather than importing `golang.org/x/vuln/scan`, so the weekly scan and the pull request scan run the same binary, built from the same pin.

## Testing the tool in isolation

The tests build throwaway git repositories, and the gorelease tests make the go command fetch fixture modules straight from them. The spike showed that isolating that takes more than `GOPRIVATE`: a developer's global `go env` (a `GONOSUMDB` set for another organisation, say) overrides `GOPRIVATE`'s defaults. The fixture therefore sets:

- `GOENV=off`;
- a temporary `GIT_CONFIG_GLOBAL` with an `insteadOf` rule pointing the module path at the repository;
- `GOPRIVATE`, `GONOSUMDB` and `GONOPROXY` for the module path;
- `GOPROXY=off` and `GIT_ALLOW_PROTOCOL=file`;
- a private module cache.

## CI layout

- **Two required checks.** The default branch's ruleset requires CI OK, a job that needs every other job and fails only on a failure or cancellation. Skipped matrices count as passing, so a PR touching no module still merges. It also requires PR title, from its own workflow. That workflow also runs on title edits, and running all of CI on every edit would be wasteful. Skipping CI's jobs on edits would be worse: CI OK would pass for a head commit whose lint had failed.
- **Cached binaries.** Jobs restore the tool and gorelease from a cache keyed on everything they're built from, the composite action included, instead of compiling in every job.
- **One publish at a time.** Publishes run one after another, and `concurrency.queue: max` keeps every waiting run. With the default, a third quick merge would replace the second's pending publish.
- **Timeouts.** Every job has a timeout, and so does each gorelease run, so a stalled download can't hold the required check for hours.
