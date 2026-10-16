## 1. Rule

No library module is touched, so there are no VERSION bump, module README or go generate tasks. Commits on this branch already follow the new rule.

- [x] 1.1 Branch `adopt-conventional-commits` off `origin/main` after `git fetch`.
- [x] 1.2 In `openspec/config.yaml`, add a "Commits and pull requests" section after Versioning and releases, as in design decision 2: Conventional Commits 1.0 for commits and PR titles, the type list, the scope rule, `!`, lowercase imperative descriptions, and "the type never sets a version".
- [x] 1.3 In `CLAUDE.md`, add commit messages to the constitution row of "Read these first".

## 2. Enforcement

- [x] 2.1 Add `.github/workflows/pr-title.yaml`, as in design decision 4:
  - `pull_request` with types opened, edited, reopened and synchronize;
  - no permissions at the top, `pull-requests: read` on the job, and a timeout;
  - `amannn/action-semantic-pull-request` pinned by SHA (v6.1.1), with the type list and `subjectPattern: ^[a-z].*[^.]$` plus an error message;
  - the job named `PR title`, the check branch protection will require.
- [x] 2.2 In `.github/dependabot.yaml`, set `commit-message` to `prefix: build` and `include: scope` for gomod, and `prefix: ci` and `include: scope` for github-actions.
- [x] 2.3 Lint the workflow with actionlint (run with `go run` at the version `.github/actionlint.yaml` names, since it isn't installed). (v1.7.12 from the repo root: no findings. Run from elsewhere, its path-based ignore for `release.yaml`'s `concurrency.queue` doesn't match.)

## 3. release-ci "latest" wording

- [x] 3.1 In `tools/release/README.md`'s publish section, replace "never marked as the latest release" with what publish does and what GitHub then shows.
- [x] 3.2 In `docs/release.md`, add why releases can't avoid "latest" (design decision 6) and why commit types describe a change but never version it (design decision 3).

## 4. Docs

- [x] 4.1 In the root `README.md` release flow, say the PR title is a Conventional Commit and becomes the squash commit on `main`.

## 5. Verification

- [x] 5.1 `openspec validate adopt-conventional-commits --strict`.
- [x] 5.2 Run `go test -short ./...` across every module in `go.work`. Nothing in Go changed, so this confirms nothing else did.
- [x] 5.3 Open the PR with a conventional title. The new `PR title` check runs on it and passes, and CI OK passes. (PR #7 on 08b3b28: `PR title` passed on its first run, with Plan, Release check, tools/release and CI OK green, and lint and test skipped since no module changed.)
- [x] 5.4 Retitle the PR to a free-form title: `PR title` fails without a new commit. Restore the title: it passes again. ("Require conventional commit titles": run 38843399048 failed with "No release type found in pull request title". Restored: run 38843427094 passed. Head stayed 08b3b28. The `subjectPattern` regex was checked locally with node against six sample subjects, all as expected, including a capitalised Dependabot-style "Bump …" that fails.)

## 6. Rollout

- [ ] 6.1 You set squash merges to use the PR title and PR body (`squash_merge_commit_title=PR_TITLE`, `squash_merge_commit_message=PR_BODY`), confirmed through the API.
- [ ] 6.2 You add `PR title` to main's required status checks next to CI OK, confirmed through the API.
- [ ] 6.3 After merge, `main`'s new commit subject is the PR title, and its body is the PR body.
- [ ] 6.4 The next Dependabot PR is titled `build(deps): …` or `ci(deps): …` and passes `PR title`. If Dependabot keeps "Bump" capitalised, follow the Risks entry in design.md.
