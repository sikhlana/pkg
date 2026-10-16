## Context

See proposal.md for why. The requirements are in specs/release-ci/spec.md. Current state that shapes the approach:

- **Squash is the only merge method.** The squash title setting is `COMMIT_OR_PR_TITLE`, and the message is `COMMIT_MESSAGES`. GitHub uses a single-commit PR's commit subject as the title, so a check on PR titles alone wouldn't cover what lands on `main`.
- **`ci.yaml` and the required check.** `ci.yaml` runs on `pull_request` with the default activity types (opened, synchronize, reopened). Branch protection requires one check, CI OK, which passes when the other jobs pass or are skipped.
- **Dependabot updates github-actions and `tools/release`'s Go modules.** Its PR titles follow its default "Bump X from A to B in /dir".
- **What publish does about "latest".** It sends `make_latest: "false"`. After `cli/v0.1.0`, GitHub reported v0.1.0 as latest. After `cli/v0.2.0`, it reported v0.2.0 as latest and v0.1.0 as not (add-cli tasks 9.4 and 10.9).

## Goals / Non-Goals

**Goals:**

- Every subject that lands on `main` is a Conventional Commit, checked by machine.
- Release notes, built from commit subjects, gain types and scopes without any change to publish.
- release-ci's spec says only what publish can guarantee.

**Non-Goals:**

- **Deriving versions or changelogs from commit types.** docs/release.md rejected commit-message release tools; `VERSION` and the gorelease floor stay the only version inputs.
- **Checking branch commits.** Squash discards them, so only the PR title is checked.
- **Validating scopes against the module list.** The check accepts any scope, and the constitution says which to use. A scope list in the workflow would have to track go.work by hand.
- **Rewriting existing history.**

## Decisions

### 1. The PR title is the commit that lasts

Squash merges put one commit on `main` per PR. Setting `squash_merge_commit_title` to `PR_TITLE` makes its subject always the PR title, whatever the branch's commits say. That makes the title the single thing to check. `squash_merge_commit_message` becomes `PR_BODY`, as the user chose: the PR description travels with the commit, including any `BREAKING CHANGE:` footer. The checklists and generated-by line come along with it.

Alternatives:
- **A blank body** gives a cleaner `git log`, but loses the description.
- **The commit list** (today's setting) carries branch commits such as release check probes.
- **The action's `validateSingleCommit`** works around `COMMIT_OR_PR_TITLE` instead of fixing it, and the action's own README prefers the repo setting.

### 2. Types and scopes

- **The types** are Conventional Commits' common set: feat, fix, perf, refactor, test, docs, build, ci, chore, revert. `build` covers go.mod and dependency changes; `ci` covers workflows and `tools/release`.
- **The scope** names where the change lives: a module's or tool's directory (`cli`, `http/server`, `tools/release`), or `openspec` for spec-only changes. It's left out when a change spans several.
- **`!`** marks a breaking change to a module's exported API at any version. Before 1.0 such a change takes a minor bump, but a reader still needs to see it's breaking.
- **The description** is lowercase and imperative, with no trailing period, matching the constitution's lowercase error style and Dependabot's prefixed titles.

### 3. Types describe; VERSION decides

A `feat(cli)` PR that adds exports still bumps `cli/VERSION` by hand, and the gorelease floor still checks it. The type and the bump can disagree (a `fix` that's a behaviour change worth a minor), and the author's VERSION wins. Release notes gain the type and scope for free, because publish already lists commit subjects. This rationale goes to docs/release.md.

### 4. A separate workflow, its own required check

`pr-title.yaml` runs `amannn/action-semantic-pull-request` v6.1.1, pinned by SHA like the repo's other actions. It runs on `pull_request` with types opened, edited, reopened and synchronize, with `pull-requests: read` permission only.

- **Not part of `ci.yaml`.** Adding `edited` there would re-run every job on each title or body edit, or skip them on edits. Skipping is worse: CI OK passes when its dependencies are skipped, so an edit-triggered run would show CI OK green for a head commit whose lint had failed. A separate workflow gives a separate check, which the user adds to branch protection.
- **`synchronize` is included** because required checks attach to the head commit. Without it, a push would leave the new head with no title check result.
- **`pull_request`, not the `pull_request_target` the action's README suggests.** That suggestion is for fork-based workflows. The title check needs only read access, which `pull_request` has for fork and Dependabot PRs alike. Unlike `pull_request_target`, it also runs in the PR that adds it, so this change tests its own check.
- **Lowercase, no period** is the action's `subjectPattern`, `^[a-z].*[^.]$`, with an error message that says so.
- **The action beats hand-rolled code.** A shell regex would be short, but would misparse what the action's Conventional Commits parser handles: `!`, nested scope paths, and the error messages. The constitution prefers a maintained tool for a solved problem.

### 5. Dependabot speaks the same format

`commit-message: { prefix: build, include: scope }` for gomod and `{ prefix: ci, include: scope }` for github-actions give `build(deps): bump …` and `ci(deps): bump …`. Dependabot adds the colon after a prefix that ends in a letter, and `include: scope` adds `deps`.

### 6. "Latest" is GitHub's call, not publish's

Publish keeps sending `make_latest: "false"`. Leaving it out would let GitHub mark every new release latest explicitly, which is what the fallback shows anyway. Sending it keeps publish from claiming a choice it doesn't make. The spec now promises only that publish never asks for latest, and states GitHub's fallback.

Alternative rejected: marking releases as prereleases, which hides the badge but misdescribes stable releases and is excluded from `/releases/latest` in ways consumers might rely on.

## Risks / Trade-offs

- **[Dependabot's capitalisation]** GitHub's docs don't say whether "Bump" becomes lowercase after a prefix. → The title check would fail such a PR visibly. Then the fix is a `prefix` ending in a space plus our own scope text, or an exception for Dependabot's login.
- **[A second required check]** Branch protection needs updating by hand, or titles go unchecked. → It's a task, confirmed through the API.
- **[PR_BODY noise]** Squash commits carry test-plan checklists and the generated-by line. → Accepted by choice; the body can be trimmed in the merge dialog.
- **[Fork PRs run with fork-scoped tokens]** Read access is enough for the action. → Nothing to do.

## Migration Plan

1. Merge this change. Its own PR title must pass the new check, which runs because the trigger is `pull_request`.
2. The user sets the squash commit title to PR_TITLE and the message to PR_BODY, and adds the title check to branch protection. Both are confirmed through the API.
3. The next Dependabot PR confirms the `build(deps):` and `ci(deps):` titles.

Rollback: delete the workflow and the required check, and revert the constitution section. Nothing depends on the format.

## What actually happened

No decision changed during implementation. Two things read differently now:

- **The scope rule moved on later.** #7 shipped decision 2 as designed, scoping by directory (`http/server`). add-configvalidator (#10) then changed the constitution to scope by package name (`httpserver`), since a nested module's package name is its identity everywhere except its path and tag. The constitution is the current rule.
- **The required checks live in a ruleset.** This design says "branch protection", but main has none. The ruleset add-release-ci created gained `PR title` beside `CI OK`, and was renamed "ci-ok + pr-title".

Rollout, verified:
- PR #7's own title passed `PR title` on its first run. Retitled free-form, the check failed without a new commit; restored, it passed (tasks 5.3 and 5.4).
- Through the API: squash merges take `PR_TITLE` and `PR_BODY`, and the ruleset requires `CI OK` and `PR title`, both from GitHub Actions.
- e5f7bd6, #7's squash commit, has the PR title plus ` (#7)` as its subject. Its body is the PR body word for word, rewrapped at 72 columns, with the branch's `Co-authored-by` trailer appended after a `---------` line.
- #8, #9 and #10 merged with conventional titles.

Follow-ups left open:
- **Task 6.4, Dependabot's titles.** No Dependabot PR has opened since the merge. If the first one keeps "Bump" capitalised, the title check fails it, and the Risks entry above has the fix.
- **"Branch protection" in workflow comments.** `ci.yaml` and `pr-title.yaml` still use it loosely for the ruleset. Harmless, but "ruleset" would be exact.

## Fixed at archive

- **"Pull request results are reported in one place" contradicted the new requirement.** It promised a single overall status that fails when any CI job fails, but the title check is a second required status that CI OK doesn't include. The delta gained a MODIFIED entry: the module jobs roll up into one status, the title check reports as its own, and a "Title edited" scenario says an edit re-runs only the title check. The proposal's capability list says so too.

## Rollup at archive

Done:
- docs/release.md gained decisions 3 and 6 in the change itself, with decision 4's separate check under CI layout. At archive, its "Branch protection requires CI OK" became the default branch's ruleset.
- The constitution holds decision 2, as since amended.
- Decisions 1 and 5 live where they apply: the constitution and `pr-title.yaml`'s header comment for the squash title, and `dependabot.yaml`'s comments for the prefixes.
