## Purpose

Release CI decides what a pull request must satisfy before it merges, and what a push to main publishes. Every library module's version and tag are then checked by machine, never by hand.

## ADDED Requirements

### Requirement: Library modules are the directories listed in go.work

CI SHALL treat exactly the directories listed in the root go.work as library modules. Directories under `tools/` SHALL NOT be library modules, even when they contain a go.mod.

#### Scenario: Tooling is not a library module
- **WHEN** `tools/release` contains a go.mod and go.work lists only `logger`
- **THEN** CI treats `logger` as the only library module, and never versions, tags or releases `tools/release`

### Requirement: Module tags follow the go command's layout

A module's tag for a version SHALL be `<dir>/v<version>`, except that a module in a major version directory (`<dir>/vN`, with module path ending in `/vN`) SHALL drop that directory: the go command looks for `logger/v2`'s versions under `logger/`. A module's releases SHALL be the tags under its prefix whose major version its module path takes, so modules sharing a prefix never see each other's versions.

#### Scenario: Major version directory
- **WHEN** `logger/v2`, with module path ending in `/logger/v2`, is at VERSION `2.1.0`
- **THEN** its tag is `logger/v2.1.0`

#### Scenario: Shared prefix
- **WHEN** the tags `logger/v1.4.0` and `logger/v2.0.0` exist
- **THEN** `logger`'s highest release is `1.4.0` and `logger/v2`'s is `2.0.0`

### Requirement: Pull requests select the modules they change

For a pull request, CI SHALL treat a library module as changed when its contents differ between the merge base and the pull request head. A file SHALL count for the module that owns it in each layout it exists in, so a file whose owner changes, because a go.mod was added or removed below a module, changes both owners. A change to root configuration (`.go-version`, `.golangci.yml` or `go.work`) SHALL select every library module for lint and test. Version rules SHALL apply only to modules whose own contents changed.

#### Scenario: One module changed
- **WHEN** a pull request edits only files under `logger/`
- **THEN** lint, test and version checks run for `logger` and for no other module

#### Scenario: Root configuration changed
- **WHEN** a pull request edits only `.golangci.yml`
- **THEN** every library module is linted and tested, and no module needs a VERSION bump

#### Scenario: No module changed
- **WHEN** a pull request edits only files outside every library module and outside root configuration
- **THEN** per-module lint and test are skipped, and the pull request's overall check still passes

#### Scenario: Package moved into a nested module
- **WHEN** a pull request adds `logger/sub/go.mod`, turning the package `logger/sub` into its own module
- **THEN** both `logger/sub` and `logger` are changed, since `logger` loses the package

#### Scenario: Nested module deleted
- **WHEN** a pull request deletes the module `http/server` and every file under it
- **THEN** `http` is not changed

### Requirement: Lint and test see published siblings only

CI SHALL lint and test each selected module the way a consumer would build it: sibling library modules resolve at the versions its go.mod requires, never from the working tree.

#### Scenario: Module relies on an unreleased sibling change
- **WHEN** `logger`'s tests pass only against `kernel`'s working tree, not against the `kernel` version in `logger/go.mod`
- **THEN** `logger`'s test job fails

### Requirement: Changed modules carry a valid VERSION

Every changed library module SHALL have a VERSION file holding a semantic version of the form MAJOR.MINOR.PATCH, with no `v` prefix, pre-release or build suffix.

#### Scenario: Missing VERSION
- **WHEN** a changed module has no VERSION file
- **THEN** the check fails, naming the module

#### Scenario: Malformed VERSION
- **WHEN** a changed module's VERSION holds `v0.2.0` or `0.2`
- **THEN** the check fails, naming the module and the invalid value

### Requirement: VERSION's major matches the module path

A changed module's VERSION SHALL have a major version its module path takes: 0 or 1 for a path without a `/vN` suffix, N for a path ending in `/vN`.

#### Scenario: v2 in place
- **WHEN** `logger`, whose module path has no major suffix, sets VERSION to `2.0.0`
- **THEN** the check fails, saying v2 lives in `logger/v2`

#### Scenario: Wrong major in a major version directory
- **WHEN** `logger/v2` sets VERSION to `0.1.0`
- **THEN** the check fails, saying its module path only takes v2 versions

### Requirement: A changed module's VERSION increases

When a library module that exists on the base branch has changed contents, its VERSION SHALL be greater than its VERSION on the base branch. This applies whatever the change: code, tests, README or godoc.

#### Scenario: Change without a bump
- **WHEN** a pull request edits only `logger/README.md` and leaves `logger/VERSION` at `0.3.0`
- **THEN** the check fails, saying `logger` changed without a VERSION bump

#### Scenario: Version moves backwards
- **WHEN** `logger/VERSION` is `0.3.0` on the base branch and `0.2.9` in the pull request
- **THEN** the check fails, saying the version is not newer than `0.3.0`

#### Scenario: Version bumped
- **WHEN** `logger/VERSION` goes from `0.3.0` to `0.3.1` and every other rule passes
- **THEN** the check passes for `logger`

### Requirement: New modules start at their first version

A library module that doesn't exist on the base branch and has never been released SHALL have its first version as VERSION: `0.1.0`, or `N.0.0` for a module path ending in `/vN`.

#### Scenario: New module at 0.1.0
- **WHEN** a pull request adds `metrics/` with VERSION `0.1.0`
- **THEN** the version check passes for `metrics`

#### Scenario: New module skips ahead
- **WHEN** a pull request adds `metrics/` with VERSION `0.2.0`
- **THEN** the check fails, saying a new module starts at `0.1.0`

#### Scenario: New major version directory
- **WHEN** `logger` has released `1.4.0` and a pull request adds `logger/v2` with VERSION `2.0.0`
- **THEN** the version check passes for `logger/v2`

### Requirement: Restored modules continue above their last release

A library module that doesn't exist on the base branch but was released before it was removed SHALL have a VERSION greater than its last release.

#### Scenario: Restored at a new version
- **WHEN** `metrics` was released at `0.5.0`, removed from main, and a pull request restores it with VERSION `0.6.0`
- **THEN** the version check passes for `metrics`, and the summary shows `0.5.0` as its last release

#### Scenario: Restored at a used version
- **WHEN** a pull request restores `metrics` with VERSION `0.5.0`, its last release
- **THEN** the check fails, saying VERSION must be newer than `0.5.0`

### Requirement: A changed module's tag is unused

The module's tag for its VERSION SHALL NOT already exist. Once a module has failed a VERSION rule, CI SHALL NOT report this or the API minimum as well.

#### Scenario: Tag already taken
- **WHEN** `logger/VERSION` is `0.3.1` and the tag `logger/v0.3.1` already exists
- **THEN** the check fails, naming the existing tag

#### Scenario: One mistake, one problem
- **WHEN** a pull request edits `logger` without bumping VERSION from `0.3.0`, which is tagged
- **THEN** the check reports only the missing bump

### Requirement: VERSION meets the minimum set by the exported-API change

For a changed module that has at least one release, CI SHALL compare the module's exported API and requirements against its highest released version. VERSION SHALL be at least the lowest version that comparison allows. The comparison's report SHALL appear in the pull request's check summary.

#### Scenario: No API change
- **WHEN** `logger`'s last release is `0.3.0`, its exported API is unchanged, and VERSION is `0.3.1`
- **THEN** the check passes for `logger`

#### Scenario: New export with a patch bump
- **WHEN** `logger`'s last release is `0.3.0`, the pull request adds an exported method, and VERSION is `0.3.1`
- **THEN** the check fails, saying the change needs at least `0.4.0`

#### Scenario: Breaking change before 1.0
- **WHEN** `logger`'s last release is `0.3.0`, the pull request removes an exported method, and VERSION is `0.4.0`
- **THEN** the check passes for `logger`, and the summary lists the incompatible change

#### Scenario: Breaking change after 1.0
- **WHEN** `logger`'s last release is `1.2.0` and the pull request removes an exported method
- **THEN** the check fails at any VERSION, saying the change needs a new major module path

#### Scenario: Unpublished sibling requirement
- **WHEN** `logger/go.mod` requires a version of `kernel` that has no release
- **THEN** the check fails for `logger`, naming the missing version

#### Scenario: The comparison itself fails
- **WHEN** the API comparison for `logger` errors for any reason
- **THEN** the check fails for `logger` and shows the comparison's output

### Requirement: Repo invariants hold on every pull request

On every pull request, whatever it changes, CI SHALL verify all of the following:
- go.work lists every directory outside `tools/` that contains a go.mod, and only those;
- every library module has a README.md and a VERSION file;
- every library module's module path is the repository's path followed by its directory;
- every library module's go directive, and go.work's, names the Go language version in `.go-version`, at any patch release.

#### Scenario: Module missing from go.work
- **WHEN** `cache/go.mod` exists and go.work doesn't list `cache`
- **THEN** the check fails, saying `cache` isn't registered in go.work

#### Scenario: Stale go.work entry
- **WHEN** go.work lists `old` and `old/go.mod` doesn't exist
- **THEN** the check fails, naming the stale entry

#### Scenario: Go version drift
- **WHEN** `.go-version` holds `1.27` and `logger/go.mod` declares `go 1.26`
- **THEN** the check fails, naming `logger/go.mod`

#### Scenario: Patch release forced by a dependency
- **WHEN** `.go-version` holds `1.27` and `logger/go.mod` declares `go 1.27.1`
- **THEN** the Go version check passes

#### Scenario: Module path doesn't match its directory
- **WHEN** the directory `logger` declares the module path `<repository>/log`
- **THEN** the check fails, naming `logger/go.mod` and the path it should declare

#### Scenario: Missing README
- **WHEN** a library module has no README.md
- **THEN** the check fails, naming the module

### Requirement: Pull request results are reported in one place

Each failure SHALL be reported against the file to fix. The check summary SHALL list every changed module with its base version, its new version and its outcome. The pull request SHALL have a single overall status that fails when any CI job fails and passes when every job passes or is skipped.

#### Scenario: Mixed results
- **WHEN** `logger` passes and `metrics` fails its version check in the same pull request
- **THEN** the summary shows `logger` as passing and `metrics` as failing with the reason, an error is attached to `metrics/VERSION`, and the overall status fails

### Requirement: A push to main releases every unreleased version

On every push to main, CI SHALL create a GitHub release, with the module's tag for its VERSION on the pushed commit, for each library module whose tag has no release yet. Modules whose current version already has a release SHALL be left untouched. If the tag exists without a release, CI SHALL create the release for the existing tag without moving it. CI SHALL NOT release a VERSION whose major the module path doesn't take.

#### Scenario: One module bumped
- **WHEN** a merge to main bumps `logger/VERSION` to `0.4.0`
- **THEN** a release `logger/v0.4.0` exists on that commit, and no other module gets a release

#### Scenario: Several modules bumped together
- **WHEN** a merge to main bumps both `logger` and `metrics`
- **THEN** each gets its own release, on the same commit

#### Scenario: Re-run
- **WHEN** the release workflow runs again for a commit whose versions are all released
- **THEN** nothing is created or changed

#### Scenario: Missed release is repaired
- **WHEN** the release for `logger/v0.4.0` failed on its merge and a later push to main leaves `logger/VERSION` at `0.4.0`
- **THEN** that later push creates the `logger/v0.4.0` release

#### Scenario: Tag without a release
- **WHEN** the tag `logger/v0.4.0` exists but has no release
- **THEN** CI creates the release for the existing tag and leaves the tag where it is

#### Scenario: Missed release overtaken by another bump
- **WHEN** the release for `logger/v0.4.0` failed on its merge and the next merge bumps `logger/VERSION` to `0.5.0`
- **THEN** CI releases `logger/v0.5.0`, never `logger/v0.4.0`, and warns that `0.4.0` was skipped

#### Scenario: Major mismatch reaches main
- **WHEN** `logger/VERSION` on main is `2.0.0` and `logger`'s module path has no major suffix
- **THEN** CI creates no release for `logger` and the publish fails

### Requirement: Releases describe only their own module

Each release SHALL be titled with its tag. Its notes SHALL list the subjects of commits that touched the module's directory since that module's previous release, or every commit that touched it, for a first release. Releases SHALL NOT be marked as the repository's latest release.

#### Scenario: Notes exclude other modules
- **WHEN** `logger/v0.4.0` is released after commits touching `logger/` and `metrics/`
- **THEN** its notes list only the commits that touched `logger/`

### Requirement: New versions are announced to the module proxy

After creating a release, CI SHALL request the new version from the public Go module proxy, so it resolves straight away. A failed request SHALL be reported as a warning and SHALL NOT fail the publish.

#### Scenario: Proxy unavailable
- **WHEN** the proxy request for `logger@v0.4.0` fails
- **THEN** the release still exists, and the run reports a warning, not a failure

### Requirement: Publishes never overlap

Publish runs SHALL execute one at a time, and a waiting run SHALL NOT be cancelled.

#### Scenario: Two quick merges
- **WHEN** two merges to main land seconds apart
- **THEN** their publish runs execute in sequence, and each new version is released exactly once
