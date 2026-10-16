## Why

Commit subjects on `main` are free-form ("Add cli.Main and CommandFunc (#6)"), and they're also what publish puts in release notes. Conventional Commits gives every subject a type and a scope, so `main`'s history and each module's release notes say what kind of change each line is. Nothing enforces the form yet, and two repo settings would undercut it:
- the squash title uses a single-commit PR's commit subject instead of the PR title;
- Dependabot titles its PRs "Bump X from A to B".

The same release process has one rule that can't hold. Publishing `cli/v0.1.0` and `cli/v0.2.0` showed GitHub marks the newest full release as latest whenever none is marked explicitly, so release-ci's "Releases SHALL NOT be marked as the repository's latest release" is false whatever publish sends.

## What Changes

- **Constitution:** a new "Commits and pull requests" section.
  - Commit messages and PR titles follow Conventional Commits 1.0, from a fixed list of types.
  - Scope is the one module or tool directory a change touches, or `openspec` for spec-only changes.
  - `!` marks a breaking API change.
  - Descriptions are lowercase and imperative, with no trailing period.
  - The type never sets a version: `VERSION` and the gorelease floor still do.
- **New workflow `pr-title.yaml`:** checks every PR title against those rules when the PR is opened, edited, reopened or pushed to. It uses `amannn/action-semantic-pull-request`, pinned by SHA.
- **Dependabot:** titles become `build(deps): …` for Go modules and `ci(deps): …` for actions.
- **release-ci's "latest" rule** is reworded to what publish controls. Publish never asks GitHub to mark a release latest, and GitHub's own fallback marks the newest one. The tools README and `docs/release.md` say the same.
- **Docs:**
  - `docs/release.md` explains why commit types describe a change but never version it;
  - the root README's release flow and CLAUDE.md's constitution row mention the commit rule.
- **Repo settings, applied by hand:**
  - squash merges take the PR title and PR body;
  - the PR title check becomes a required status check.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-ci`:
  - a new requirement that pull request titles follow Conventional Commits;
  - "Releases describe only their own module" no longer claims releases are never marked latest.

## Impact

- **Library modules:** none touched, so no exported API changes and no VERSION bumps. `cli` is untouched.
- **`tools/release`:** README only. No code changes; publish keeps sending `MakeLatest: "false"`.
- **New dependency (CI only):** amannn/action-semantic-pull-request, pinned by commit SHA and kept current by Dependabot's github-actions updates.
- **Files:**
  - `openspec/config.yaml`;
  - `.github/workflows/pr-title.yaml` and `.github/dependabot.yaml`;
  - `README.md`, `CLAUDE.md`, `docs/release.md` and `tools/release/README.md`.
- **GitHub:** the squash-merge setting and branch protection change by hand. Existing history isn't rewritten; the rule applies from this change on.
