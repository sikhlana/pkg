## ADDED Requirements

### Requirement: Pull request titles are Conventional Commits

CI SHALL fail a pull request whose title isn't a Conventional Commits 1.0 header with one of the types feat, fix, perf, refactor, test, docs, build, ci, chore or revert. A header's scope and `!` breaking marker are optional. CI SHALL also fail a title whose description starts with an uppercase letter or ends with a period. The check SHALL run again whenever the title is edited or new commits are pushed, so the result always reflects the current title on the current head commit.

#### Scenario: Conventional title passes
- **WHEN** a pull request is titled `feat(cli): add Main and CommandFunc`
- **THEN** the title check passes

#### Scenario: Breaking marker passes
- **WHEN** a pull request is titled `feat(cli)!: drop Exitf`
- **THEN** the title check passes

#### Scenario: Free-form title fails
- **WHEN** a pull request is titled `Add cli.Main and CommandFunc`
- **THEN** the title check fails

#### Scenario: Unknown type fails
- **WHEN** a pull request is titled `feature(cli): add Main`
- **THEN** the title check fails

#### Scenario: Capitalised or punctuated description fails
- **WHEN** a pull request is titled `fix(cli): Handle nil errors.`
- **THEN** the title check fails

#### Scenario: Fixing the title clears the failure
- **WHEN** a pull request whose title check failed is retitled `fix(cli): handle nil errors`
- **THEN** the title check runs again and passes, without a new commit

#### Scenario: Dependabot titles pass
- **WHEN** Dependabot opens a pull request bumping a Go dependency of `tools/release`
- **THEN** its title starts with `build(deps): ` and the title check passes

## MODIFIED Requirements

### Requirement: Releases describe only their own module

Each release SHALL be titled with its tag. Its notes SHALL list the subjects of commits that touched the module's directory since that module's previous release, or every commit that touched it, for a first release. Publish SHALL NOT ask GitHub to mark a release as the repository's latest. GitHub still treats the newest full release as latest when none is marked, so in practice the most recently published release of any module is shown as latest.

#### Scenario: Notes exclude other modules
- **WHEN** `logger/v0.4.0` is released after commits touching `logger/` and `metrics/`
- **THEN** its notes list only the commits that touched `logger/`

#### Scenario: Latest is never requested
- **WHEN** publish creates `cli/v0.2.0`
- **THEN** the request tells GitHub not to make it the latest release, and whatever GitHub then shows as latest comes from its own fallback
