## 1. Module scaffold

`cli` is new, so it starts at 0.1.0 with no bump task. It has no mocked interfaces, so there's no go generate task. Nothing is copied from go-modules' `cli`: code, godoc and comments are all written here, from what this code does.

- [x] 1.1 Branch `add-cli` off `origin/main` after `git fetch`.
- [x] 1.2 Create `cli/`:
  - `go.mod` for `github.com/sikhlana/pkg/cli`, with the go directive from `.go-version`;
  - `VERSION` containing `0.1.0`;
  - require the latest spf13/cobra and stretchr/testify.
  - Done when `go build ./...` passes from `cli/`.
- [x] 1.3 Register `./cli` in the root `go.work`.

## 2. Signals

- [x] 2.1 `SignalContext(parent)`, as in design decision 1. Its godoc covers the second signal and the "another subscriber keeps the handler" caveat.
- [x] 2.2 Unix-only tests (`//go:build unix`), none parallel:
  - the first SIGINT to the test process cancels the context, and `context.Cause` names the signal;
  - cancelling the parent ends the context;
  - a child re-run of the test binary is killed by a second SIGINT, judged by its wait status.

## 3. Exit codes

- [x] 3.1 `ExitError` with pointer receivers (`Error` gives `exit status N` when `Err` is nil; `Unwrap`), and `Exit(code, err)`. See design decision 3.
- [x] 3.2 `Execute(ctx, cmd)`, as in design decision 2.
- [x] 3.3 Tests:
  - success returns 0;
  - a plain error returns 1;
  - a wrapped exit code is returned;
  - a zero or negative code returns 1;
  - the message with a nil `Err`;
  - `errors.Is` reaches the cause;
  - a context value reaches `RunE`.

## 4. Build info

- [x] 4.1 Link-time variables `Version`, `Revision` and `Time`, defaulting to empty. Also the `BuildInfo` type with `String()`, `ReadBuildInfo()`, and the pure resolver it calls. See design decision 7.
- [x] 4.2 `export_test.go` exposing the resolver, plus tests for:
  - per-field precedence (link-time, then stamped, then default);
  - `(devel)` treated as unset;
  - missing VCS keys;
  - `Modified`;
  - `String()` with all details, some, and none (no parentheses).

## 5. Commands

- [x] 5.1 The `ConfigFlag` constant and `NewRootCommand(name, short)`: `SilenceUsage`, persistent `--config`/`-c`, `Version` set from `ReadBuildInfo().String()`, and the version template. See design decisions 5, 6 and 8.
- [x] 5.2 `NewVersionCommand()`, printing the root's display name and `ReadBuildInfo().String()`. See design decision 8.
- [x] 5.3 Tests through `SetOut`, `SetErr` and `SetArgs`:
  - a runtime error prints once without usage;
  - a flag error prints without usage;
  - a quiet exit prints nothing and returns its code;
  - `--version` output equals the `version` subcommand's;
  - `-c` reaches a subcommand;
  - `--help` still prints help.

## 6. Constitution and docs

- [x] 6.1 In `openspec/config.yaml`, add the error-type exception to the Code standards receiver rule: error types use pointer receivers so `errors.AsType` has a single target.
- [x] 6.2 Write `cli/README.md` in the constitution's order:
  - overview;
  - Configuration: none, and say why;
  - Exported Members: every export, including `ExitError.Code`, `ExitError.Err`, the `BuildInfo` fields and the methods;
  - Usage, Standalone: a `main` that calls `os.Exit(run())`, link-time values for builds without `.git`, custom exit codes, quiet exits;
  - Usage, With fx: a command that runs an fx app under `cmd.Context()`.
  - No backticks in table cells.
- [x] 6.3 In the root `README.md`, add the `cli` row to the Modules table and delete "Nothing has shipped yet."

## 7. Verification

- [x] 7.1 In `cli/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 7.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 7.1 and 7.2.
- [x] 7.3 Build check by hand, before anything is published. Make a scratch git repo in a temp directory outside this repo. Its module requires `github.com/sikhlana/pkg/cli` through a `replace` to the local `cli/`; that's allowed in a scratch module, not in this repo. Commit, tag `v1.2.3`, then check each case, printing one line per case and confirming five lines:
  - plain `go build`: `--version` and `version` both print `scratch v1.2.3 (<revision>, <commit time>)`;
  - an uncommitted edit: the version gains `+dirty` and the list ends in `modified`;
  - `-ldflags "-X github.com/sikhlana/pkg/cli.Version=1.0.0-rc1"`: the version is overridden, and the revision and time are still stamped;
  - `-buildvcs=false`: `scratch dev`;
  - a command that ignores its context, sent Ctrl-C twice: the second kills it, with exit status 130.
  - Delete the temp directory afterwards.

## 8. Code review fixes

From the code review of 8c0dcd1, run before the PR. design.md records each decision; tests cover each fix.

- [x] 8.1 `SignalContext` owns its handler: one goroutine calls `signal.Stop` before cancelling, and `stop` releases synchronously (design decision 1). It no longer uses `NotifyContext` and `AfterFunc`, so the window that swallowed a second signal is gone.
- [x] 8.2 New export `SignalError`, the cancellation cause: it carries the signal and matches `context.Canceled`.
- [x] 8.3 Signal tests:
  - the first signal is checked for both SIGINT and SIGTERM;
  - the second-signal test sends SIGTERM with no sleep, so it passes when started with SIGINT ignored (`bash -c '… & wait'`);
  - the child is reaped by `t.Cleanup`, and its output is read to EOF before `Wait`;
  - new tests for `stop` called twice and the `SignalError` message.
- [x] 8.4 `Exit` returns `error`, so a nil `*ExitError` from a helper can no longer reach cobra or `Execute` as a non-nil error (design decision 3).
- [x] 8.5 Codes outside 1–255 exit 1, so `Exit(256, …)` no longer exits 0. `Error()` and `Execute` share the normalisation, so `exit status N` names the code the process really exits with. Tests cover 255, 256, and the messages for 0 and 256.
- [x] 8.6 `Execute` runs `cmd.Root()` and gives `ctx` to every command in the tree first. Tests cover a reused tree and being handed a subcommand.
- [x] 8.7 `--config` has no shorthand (design decision 6). The test for a subcommand defining its own `-c` replaces the shorthand test.
- [x] 8.8 The version subcommand prints the root's `Version`, falling back to `ReadBuildInfo`. Tests cover an overridden root `Version` and a root not built by `NewRootCommand`.
- [x] 8.9 README:
  - `SignalError` rows;
  - `Exit` returns `error`;
  - the 1–255 rule;
  - no `-c`;
  - the started-ignored signal exception;
  - the exit-130 snippet deleted;
  - `-X` values quoted, with an RFC 3339 commit-time command.
- [x] 8.10 The proposal, spec and design updated to match. `openspec validate add-cli --strict` passes.
- [x] 8.11 Verification chain on `cli/` (modernize, golangci-lint with and without `--fix`, full `go test`), plus `go test -count=20 -race -run Signal`, then the short sweep across `go.work`.
- [x] 8.12 Repeat the 7.3 build check with a scratch program and the new code: tagged build, uncommitted edit, `-ldflags` version, `-buildvcs=false`, a quoted `-X` time with spaces, and two real SIGINTs from a parent that doesn't ignore them (exit status -2, sent with no delay after "cancelled"). 6/6 cases passed, and the scratch directory was removed.

## 9. Release by hand

Side effects and how each is undone:
- the deliberate-failure commits are reverted with new commits, and squash merge keeps them off `main`;
- the scratch directories are deleted after each check;
- the `cli/v0.1.0` release is immutable and intended, and can't be undone. A broken v0.1.0 is fixed forward (decision in design.md, Migration Plan).

- [x] 9.1 Commit, then run `plan` and `check` locally against `origin/main` (tools/release README, "Running check locally"). `plan` prints `["cli"]`, and `check` passes: new module at 0.1.0, gorelease against no base.
- [ ] 9.2 Push and open the PR. Check `git diff --stat origin/main...HEAD` lists only the intended files. In CI, `plan` outputs `["cli"]`, lint and test run for `cli`, release check passes, and CI OK passes. Record the run.
- [ ] 9.3 Trigger each failure as a new commit, record release check's message, then revert it with another new commit. Never amend or force-push:
  - `VERSION` set to `0.2.0` (a new module must start at 0.1.0);
  - `go 1.26` in `cli/go.mod` (doesn't match `.go-version`);
  - `./cli` removed from `go.work` (go.work must list every module).
  - Confirm CI OK is green again after the last revert.
- [ ] 9.4 Squash-merge. `release.yaml`'s publish creates tag `cli/v0.1.0` on the merge commit, with a GitHub release that's titled with the tag, isn't marked latest, and has notes listing the `cli/` commits. The proxy request succeeds or warns. Record the run.
- [ ] 9.5 Consumer check against the published version, in a fresh temp module with `GOWORK=off` and no `replace`: `go get github.com/sikhlana/pkg/cli@v0.1.0` resolves through proxy.golang.org and verifies against sum.golang.org, and the 7.3 scratch program builds and prints its version line.
- [ ] 9.6 pkg.go.dev shows `github.com/sikhlana/pkg/cli@v0.1.0` with the MIT license detected. Request the page if it isn't indexed yet.
- [ ] 9.7 Re-run the publish run with `gh run rerun`. It reports nothing to create, and no new tag or release appears.
- [ ] 9.8 Bump-rule check in a follow-up PR that touches only `cli/`. It carries any fix found in 9.5 or 9.6, or else a `cli/README.md` edit worth making.
  - Pushed without a bump: release check fails.
  - With `VERSION` at `0.1.1`: it passes.
  - After merge: publish creates `cli/v0.1.1`, with notes listing only commits since `cli/v0.1.0`.
