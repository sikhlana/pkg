## Why

The release tool's multi-module subcommands work through modules one at a time. `vulncheck` runs govulncheck over every module and tool in turn, each waiting on the vulnerability database and module downloads, so the weekly job grows linearly with the repo. `check` runs gorelease, which downloads and builds, once per changed module, so a pull request that releases a tier of modules waits for each in turn. `publish` checks every module's release, writes its notes from git and announces new versions to the module proxy, one after another, on every push to main. Most of that time is spent waiting on the network or on a child process, not on the tool.

## What Changes

- **A shared worker pool for per-module work**, built on `github.com/alitto/pond/v2`. A small `parallel` package in `tools/release` runs a function over a list on a pool of bounded size and returns the results in the list's order, as one value and error per item: nothing is cancelled when one item fails, and an item whose task panicked, or never ran because the run was interrupted, gets its own error instead of aborting the rest.
- **`check`** checks its changed modules in parallel. It now checks every module instead of stopping at the first one whose check couldn't run, and returns those errors joined, so a pull request sees every broken module at once. The rule outcomes, the report and its module order are unchanged. The gorelease binary is still built at most once.
- **`publish`** makes every GitHub request, looking a release up or creating it, one at a time, in module order, as GitHub asks of one token's requests. It builds the new versions' release notes from git on the pool, and, once every release is created, announces the new versions to the module proxy on the pool. One module failing still doesn't stop the others, and the outcomes keep module order.
- **`vulncheck`** scans its modules and tools in parallel, with the same partial-results-and-`ErrIncomplete` behaviour as today.
- **`--jobs`** on all three bounds the pool. It defaults to `GOMAXPROCS`, the runner's core count, and must be at least 1. Interrupting the tool stops queued work, and running child processes stop through their context as today.
- `docs/release.md` and the tool's README say what runs in parallel and why `publish` creates serially. The `add-govulncheck` design's "declined: parallel scans" is superseded.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. The `release-ci` spec describes what a pull request must satisfy and what a push to main publishes; both are unchanged, and report order is preserved. How the tool schedules its work, and how it reports an error that stops a check from running, aren't spec-level behaviour, so this change sets `skip_specs`.

## Impact

- **`tools/release`:** new `parallel` package; `check`, `publish` and `vulncheck` use it; their `Options` gain `Jobs`; `main.go` adds `--jobs` to the three commands. Repo tooling with no VERSION.
- **New dependency:** `github.com/alitto/pond/v2` (v2.7.3: no dependencies of its own, Go 1.20), in `tools/release/go.mod` only. No library module requires it.
- **No library module changes.**
- **Docs:** `docs/release.md`, `tools/release/README.md`.
- **Release:** merging publishes nothing.
