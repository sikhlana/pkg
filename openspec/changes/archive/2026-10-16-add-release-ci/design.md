## Context

The repo has no modules, workflows or tooling yet; see proposal.md for why release CI comes first. Constraints that shape the approach:

- Each module lives in a subdirectory and is tagged `<dir>/v<version>`. There's no root go.mod, so every Go command runs from a module directory.
- The constitution makes VERSION the source of truth and bumps it in every PR that touches a module. A spike (below) showed gorelease can compute the minimum bump, but only through its suggestion, not its exit status.
- Before merge, modules must depend on published sibling versions. CI builds with `GOWORK=off` so it sees what a consumer would see.
- It's a personal, public repo: one maintainer, squash merges, and `GITHUB_TOKEN` is the only credential.

### Spike: gorelease on Go 1.27.2

Run against a throwaway repo with a module in a subdirectory tagged `logger/v0.1.0`, fetched through the real `go` command:

| Change since last tag | Suggested version | Patch bump passed with -version |
|---|---|---|
| Internal only | v0.1.1 | accepted |
| New export, v0 | v0.2.0 | accepted (exit 0) |
| Breaking, v0 | v0.2.0 | accepted (exit 0) |
| Dependency patch bump | v0.1.2 | accepted |
| Requires an untagged sibling | none | exit 1, unknown revision |
| replace directive | not flagged | accepted |
| Breaking, v1 | none | exit 1, incompatible changes |
| New export, v1 | none | exit 1, minor not incremented |

The suggested version always matched the constitution's bump rules, including dependency updates being a patch. At v0, though, gorelease's exit status accepts any bump. A module with no tags needs `-base=none`. The only output noise was a "-d flag is deprecated" warning from its internal `go get`.

Running it in isolation took more setup than expected. A developer's global `go env` (here, `GONOSUMDB=github.com/idio/*`) overrides `GOPRIVATE`'s defaults, so the test harness must set `GOENV=off`. The harness also needs: a temporary `GIT_CONFIG_GLOBAL` with a `url.<file://repo>.insteadOf` rule; `GOPRIVATE`, `GONOSUMDB` and `GONOPROXY` for the module path; and `GIT_ALLOW_PROTOCOL=file`.

## Goals / Non-Goals

**Goals:**

- Every rule in the release-ci spec is checked by code, not by a reviewer.
- Re-running any workflow is safe. A failed or skipped publish is repaired by the next push to main that leaves the module's VERSION alone, without manual tagging.
- PR feedback reads well in the job summary, with errors pointing at the file to fix.

**Non-Goals:**

- Changelog files, conventional commits, or batched release PRs.
- govulncheck, coverage, and CI on push to main (beyond publishing). Each can be added later without changing this design.
- Dependabot for library modules. A dependency bump there needs a VERSION bump, which Dependabot can't make.
- Releasing `tools/release` itself.

## Decisions

### 1. You set the version; the API diff sets a minimum

The PR author bumps VERSION. `check` runs gorelease against the module's last tag, reads its suggested version, and fails if VERSION is lower. The author can go higher (a behaviour change with an unchanged API still needs judgment), never lower.

- *Alternative: release-please.* Conventional commits decide the bump and a bot opens release PRs. Rejected: it moves the bump out of the feature PR, adds a PR to every release, and needs a token beyond the default one for CI to run on bot PRs.
- *Alternative: versions computed entirely from the API diff.* Rejected: it can't tell a behaviour change from a refactor.

### 2. The minimum comes from gorelease's suggestion, not its exit status

The spike showed `-version` validation accepts any bump at v0. `check` runs gorelease without `-version`, parses the `Suggested version: vX.Y.Z` line, and compares it with `golang.org/x/mod/semver`. A non-zero exit fails the module, with the output shown, as does a missing suggestion (breaking change at v1, unpublished dependency). This covers the user's decision to block when gorelease breaks.

The base is passed explicitly: `-base=v<highest semver tag for the module from git>`, or `-base=none` when there are no tags. That avoids gorelease's proxy-based lookup, which can briefly lag a tag pushed minutes earlier.

Whether a module is new depends on two things: whether it exists on the base branch, and whether it has releases.

- **New** (absent on base, never released): VERSION must be the path's first version, `0.1.0`, or `N.0.0` for a module path ending in `/vN`. A fixed `0.1.0` would make the `/v2` migration the check itself recommends impossible. gorelease with `-base=none` suggests v2.0.0 for a `/v2` path, and Go would reject a `logger/v2/v0.1.0` tag anyway.
- **Restored** (absent on base, but released before it was removed): VERSION must be above the last release. Treating it as new would demand `0.1.0`, which collides with its old tags and gorelease's minimum.
- **Existing:** VERSION must be above the base branch's.

"Untagged" was never the right test. If a module merged at 0.1.0 and its publish failed, the next PR touching it would have to be both above 0.1.0 and equal to it. With publish-on-merge, the constitution's "edits before its first tag don't bump" only applies inside the module's first PR. The constitution's wording changes to match.

### 3. Publishing creates any tags that should exist and don't

`publish` ignores what the push changed. For every library module it compares `<dir>/v<VERSION>` with existing releases and creates whatever is missing, on the pushed commit.

This is correct because of the bump rule: a module's files never change without its VERSION changing. So every commit with the same VERSION has identical module contents, and it doesn't matter which one gets the tag. A failed run is repaired by the next push that leaves the module's VERSION alone. A tag without a release (hand-made, or a crash between steps) gets its release created.

If the next merge bumps the module again before a repair, the missed version is skipped: `publish` only ever looks at the current VERSION. That's accepted. Go handles version gaps, and the later release contains everything the skipped one would have. `publish` reads VERSION's history since the previous release and warns about each skipped version, so a gap is never silent. Tagging historic commits would mean releasing versions nobody reviewed as a release.

One known hole in "same VERSION, same contents": the root LICENSE. go copies it into every module's zip, but editing it doesn't select any module, so a repair could tag a later commit whose LICENSE differs. That's accepted too. Consumers only ever see the tagged commit's zip, which is immutable. Making every module bump for a LICENSE edit, such as the yearly copyright line, would cost far more than the hole.

`publish` reads VERSION files from the working tree and tags HEAD, so it refuses to run when tracked files differ from HEAD. There's no flag to tag another commit: a `--sha` that didn't match the checkout would tag one commit with another commit's VERSION.

- *Alternative: diff against `github.event.before` (go-modules).* Rejected: a run that fails partway isn't repaired by re-running or by later pushes.

### 3a. Tags and majors follow the go command

For a module in a major version directory (`logger/v2`, path ending in `/logger/v2`), the go command looks for tags under `logger/`, not `logger/v2/`. The tool derives each module's tag prefix the same way: the directory, minus a trailing `/vN` that matches the path's major. So `logger/v2` at 2.1.0 is tagged `logger/v2.1.0`, and gorelease's own suggestion ("with tag logger/v2.0.0") agrees, as the integration test checks.

`logger` and `logger/v2` share that prefix, so a module's releases are the tags under its prefix whose major `module.CheckPathMajor` accepts for its path. Without the filter, `logger` would treat `logger/v2.0.0` as its latest release, and every later gorelease run against it would fail.

The same check guards VERSION: a major the path doesn't take (2.0.0 on a path without `/v2`) fails `check` and is refused by `publish`. Otherwise it would produce a tag the go command rejects, and immutable releases would make it permanent.

Two new repo invariants back this up:
- **Module path:** every module's path must be the repository path followed by its directory. The repository path is the release tool's own module path minus `tools/release`. This rules out a directory whose tags the go command would never look under. It also puts a new major in its own `<dir>/vN` directory rather than in place.
- **Go version:** go directives are compared by language version (`go/version.Lang`), so a dependency forcing `go 1.27.1` doesn't fail against `.go-version`'s 1.27.

### 4. A Go tool in `tools/release`, outside go.work

`tools/release` is its own module with no VERSION and isn't listed in go.work, so the module list (taken from go.work) never includes it. Workflows run it as `GOWORK=off go run . <command>` from its directory, and it finds the repo root with `git rev-parse --show-toplevel`. Repo tooling under `tools/` follows the constitution's code standards but not its module-release rules.

- *Alternative: bash in the workflow YAML.* Rejected: it hand-rolls semver comparison, previous-tag lookup, go.mod parsing and annotation escaping, the fiddly problems the constitution wants a library for.
- *Alternative: list it in go.work and treat "has VERSION" as "releasable".* Rejected: it would make library modules and tools look alike to every workspace command.

### 5. gorelease is pinned in the tool's go.mod

`tool golang.org/x/exp/cmd/gorelease` pins it through the tool's go.mod and go.sum, and Dependabot updates it. `go tool` only runs from the module that declares the tool, but gorelease must run in each library module's directory. So the tool runs `go build -o <temp>/gorelease golang.org/x/exp/cmd/gorelease` from its own directory once, then runs that binary with the library module as its working directory and `GOWORK=off`.

Each run has a 10-minute timeout, and every CI job has `timeout-minutes`. Without them, a stalled module download would hold the required `CI OK` check for GitHub's 6-hour default.

### 6. Dependencies and why each beats hand-rolled code

| Dependency | Used for | Instead of |
|---|---|---|
| golang.org/x/mod | semver comparison, go.mod and go.work parsing, escaping module paths for proxy URLs | Splitting versions on dots, regexes over go.mod |
| google/go-github | Looking up releases, creating releases | Raw REST calls through the gh CLI |
| spf13/cobra | Subcommands and flags | Hand-parsing arguments |
| sethvargo/go-githubactions | Job summary, error annotations, step outputs | Hand-escaping workflow commands |
| gorelease (tool directive) | The API-diff minimum | Reimplementing apidiff and base-version loading |
| testify, gomock | Tests, per the constitution | n/a |

Git itself is driven through the `git` CLI (`diff --name-only`, `tag -l`, `log`). It's already on every runner, and go-git would add weight and differ subtly from what developers run.

### 7. One API call creates the tag and the release

`publish` calls `CreateRelease` with `tag_name`, `target_commitish` set to the pushed SHA, and `make_latest=false`. GitHub creates the tag as part of the release, so the tag-push step and its failure mode go away. "Latest" is meaningless across independent modules.

- *Alternative: push the tag with git, then create the release.* Rejected: two steps that can fail between them.

### 8. Release notes cover only the module's own commits

Notes are `git log --format=%s <prev>..<sha> -- <dir>`, where `<prev>` is the module's highest semver tag (via x/mod/semver). With squash merges, each line is a PR title, and GitHub links the `(#N)` reference. A first release lists every commit touching the directory.

- *Alternative: GitHub's generated notes.* Rejected: they list every PR merged between the two tags, across the whole repo.

### 9. Asking the module proxy for each new version

After each release, `publish` requests `https://proxy.golang.org/<escaped path>/@v/<version>.info`, escaping the path with `module.EscapePath`. That makes the version known to the proxy, the checksum database and pkg.go.dev right away. A failure is a warning, not an error: the release already exists.

### 10. Testing only changed modules is enough with GOWORK=off

`plan` diffs `merge-base(base, HEAD)..HEAD`. A change to root config (`.go-version`, `.golangci.yml`, `go.work`) selects every module for lint and test, but doesn't require VERSION bumps.

A module is changed when its contents change, judged against both the base layout and the PR's:
- an added file counts for its owner at HEAD;
- a deleted file counts for its owner at the merge base;
- a modified file counts for both;
- an unchanged file whose owner moved counts for both owners.

Judging by HEAD's layout alone missed two cases:
- **Carving a package into a nested module** (adding `logger/sub/go.mod`) silently drops a package from `logger`, a breaking change, without marking `logger`.
- **Deleting a nested module** outright marked its parent for no reason.

Under `GOWORK=off`, a module sees only its siblings' published versions, so a PR can't break an unchanged module in CI. The local `go.work` sweep remains the early warning for siblings that will upgrade later.

### 11. Workflow layout

```
pull_request ─▶ ci.yaml
                ├─ plan ─────────▶ changed modules (JSON) ─┬─▶ lint  (matrix, GOWORK=off)
                │                                          └─▶ test  (matrix, GOWORK=off)
                ├─ release-check   (release check: gorelease, invariants)
                ├─ tools-release   (lint + test tools/release)
                └─ ci-ok           (needs all; the one required check;
                                    skipped matrices count as passing)

push main ────▶ release.yaml
                └─ publish         (contents: write, concurrency "release", never cancelled)
```

- Jobs get read-only permissions by default.
- Actions are pinned by commit SHA, with a version comment.
- `setup-go` reads `.go-version` with `GOTOOLCHAIN=local`.
- golangci-lint's version is pinned to the one developers run locally.
- Every job has `timeout-minutes`.
- The release-tool action's cache key also hashes the action itself, so changing how it builds the binaries can't keep serving stale ones.
- Dependabot's gomod entry allows every dependency type. gorelease's module is required only by the tool directive, so it's marked `// indirect`, and the direct-only default would never bump it.

### 12. Testing the tool

- Unit tests build fixture git repos in `t.TempDir()`.
- The GitHub client sits behind a small interface mocked with gomock (generated by `//go:generate`).
- gorelease-backed tests use the isolation recipe from the spike, build gorelease, and skip under `-short`.

## Risks / Trade-offs

- **gorelease is experimental and ageing.** It still runs a deprecated `go get -d`. → Pinned by the tool directive and blocking by choice. If a Go release breaks it, the Go version stays put or gorelease's pin moves; the failure is visible, never silent.
- **The bump rule could be bypassed by a direct push to main.** → Branch protection requires the CI OK check (job `ci-ok`) and up-to-date branches. Squash-only merging keeps one commit per PR.
- **Immutable releases can't be retagged.** → Fix forward with a new patch version, which matches the Go proxy's own immutability.
- **Behaviour changes with an unchanged API pass the minimum at patch.** → The author still chooses; the minimum only stops under-bumping.
- **Releases made with `GITHUB_TOKEN` don't trigger other workflows.** → Nothing depends on them.
- **`tools/release` isn't in the local go.work sweep.** → CLAUDE.md gets its own test command; CI tests it in `tools-release`.

## Migration Plan

1. Merge the workflows, `tools/release`, Dependabot config and doc updates. With no modules, `plan` returns nothing and `publish` is a no-op.
2. Apply the repo settings by hand: squash only, up-to-date branches, the CI OK check required, immutable releases.
3. The first module's PR exercises `check`; its merge exercises `publish`.

Rollback: delete the workflows. Releases already published stay, by design.

## Open Questions

- Whether to run lint and test on push to main as well, to catch problems from merges landing close together. Deferrable while one person merges with up-to-date branches required.

## What actually happened

Deviations from the design:

- **The workflows run a cached binary, not `go run`** (decision 4). A local composite action, `.github/actions/release-tool`, builds the tool and its pinned gorelease, and caches both under a key hashing everything they're built from. `check --gorelease` takes the prebuilt binary; building on demand (decision 5) remains the local default.
- **The release workflow sets `concurrency.queue: max`.** With only `cancel-in-progress: false`, GitHub keeps one pending run per group, so a third quick merge would replace the second's pending publish. actionlint doesn't know the key yet, so `.github/actionlint.yaml` ignores it.
- **`plan` became a third command,** so lint and test start without waiting for gorelease. The proposal was updated to match.
- **`wsl_v5` was enabled during implementation,** and the constitution gained the empty-line rule it enforces.
- **The code review reshaped decisions 2, 3, 3a, 5, 10 and 11** (next section). Most visibly: tags follow the go command's layout, and `publish` tags HEAD with no `--sha`.

Rollout, verified:
- PR #1's CI passed: CI OK passed, and lint and test were skipped with no modules.
- The release run on the merge commit `ba033b8` created nothing.
- The repo settings were confirmed through the API:
  - squash merge only;
  - an active ruleset on the default branch requiring CI OK with up-to-date branches, and blocking deletion and force pushes;
  - immutable releases enabled.

Follow-ups left open:

- Lint and test on push to main (the open question below), still deferred.
- govulncheck and coverage, out of scope here.
- actionlint runs only locally. Adding it to CI, and dropping the `queue` ignore once actionlint supports the key, are both worth doing.
- gorelease is experimental and pinned, so it needs checking on each new Go release.
- The ruleset has no bypass actors, so archive and docs commits also go through a PR. That's intended.

## Fixes from code review

The review of the first implementation found fifteen issues; all are fixed or settled:

- **Tags and majors** (decision 3a): VERSION's major is checked against the module path; tags drop a major version directory; releases are filtered by major; and the module path must be the repository path plus the directory.
- **New and restored modules** (decision 2): the first version follows the path's major, and a module with releases is never "new".
- **Ownership** (decision 10): computed against both layouts, so nested-module carve-outs and deletions are handled.
- **Go versions:** compared by language version.
- **publish:** always tags HEAD and refuses a dirty tree; `--sha` is gone.
- **Accepted, now documented and warned about** (decision 3): skipped versions and the LICENSE hole.
- **Infrastructure:** gorelease timeout, job timeouts, the cache key, and Dependabot's `allow` (decisions 5 and 11).
- **Cleanups:**
  - the tag check and gorelease are skipped once VERSION fails;
  - one markdown cell escaper, `report.Cell`, which also escapes newlines;
  - `version.Below` over the already-sorted list;
  - gorelease's temp directory is only created when check builds it.

## Rollup at archive

Done: `docs/release.md` now holds this design's lasting reasoning, which was:
- the decision to create missing tags, and the bump-rule property that makes it correct;
- the gorelease minimum and the spike's quirks;
- why testing changed modules is enough under `GOWORK=off`;
- the test isolation recipe.
