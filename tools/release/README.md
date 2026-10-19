# release

The repository's release CI. Pull requests run `plan` and `check`; every push to `main` runs `publish`; a weekly workflow runs `vulncheck`. It's repo tooling under `tools/`, so it sits outside `go.work`, runs with `GOWORK=off`, has no `VERSION` and is never released. [docs/release.md](../../docs/release.md) explains why it's shaped this way.

## Commands

### plan

Lists the library modules a pull request lints and tests: those whose contents changed between the merge base with `--base` and `HEAD`, or every module when `.go-version`, `.golangci.yml` or `go.work` changed. It prints, and in GitHub Actions sets as step outputs, `modules` (a JSON array of module directories, for a matrix) and `empty`.

A file counts for the module that owns it in each layout it exists in, so moving a package into its own nested module changes the parent too, while deleting a nested module outright doesn't.

### check

Fails a pull request when any of these breaks:

- Every changed module has a `VERSION` holding exactly `MAJOR.MINOR.PATCH`, whose major the module path takes: v0 or v1 without a `/vN` suffix, vN with one.
- A module that's absent on `--base` and has never been released starts at its first version: `0.1.0`, or `N.0.0` in a `<dir>/vN` directory. A module absent on `--base` that was released before it was removed continues above its last release. Any other changed module's `VERSION` is greater than on `--base`.
- The module's tag for `VERSION` doesn't exist yet. Tags are `<dir>/v<VERSION>`, except that a module in a major version directory drops it, as the go command expects: `logger/v2` at 2.1.0 is tagged `logger/v2.1.0`.
- `VERSION` is at least the version gorelease suggests from the exported-API diff, and from a go.mod that raises a requirement's minor or the go directive, against the module's highest release (or against nothing, for a module with no release). A breaking change past v1 fails at any version: it needs a new major module path.
- Whatever changed:
  - `go.work` lists exactly the directories outside `tools/` with a `go.mod`;
  - every library module has a `README.md`, a `VERSION`, and the module path `<repository path>/<dir>`, the repository path coming from this tool's own `go.mod`;
  - `go.work`'s and every module's go directive names the Go language version in `.go-version`, at any patch release.

Once a module fails a `VERSION` rule, its tag check and gorelease are skipped: they'd only repeat the same mistake.

It prints a markdown report, one row per changed module plus each gorelease report. In GitHub Actions it also writes the job summary and attaches an error annotation to the file to fix.

By default it builds the gorelease version `go.mod` pins, the first time a module needs it; `--gorelease <path>` uses a prebuilt binary instead.

It checks up to `--jobs` modules at once, by default `GOMAXPROCS`. When a module can't be checked at all (git or gorelease failing to run, not a broken rule), the others are still checked, and the command fails with every such module's error.

### publish

For every library module whose tag for `VERSION` has no GitHub release, creates one titled with the tag, and never asks GitHub to make it the latest release. GitHub still shows the newest full release as latest when none is marked, so the badge follows whichever module released most recently. A new tag points at `HEAD`; a tag that exists without a release keeps its commit. `VERSION` files are read from the working tree, so `publish` refuses to run when tracked files differ from `HEAD`, and it refuses a `VERSION` whose major the module path doesn't take. The notes list the subjects of commits touching the module's directory (not its nested modules) since its previous release, or every such commit for a first release. Each new version is then requested from proxy.golang.org; a failure there is a warning, not an error.

Versions that already have a release are left alone, so a re-run changes nothing, and a failed run is repaired by the next push that leaves the module's `VERSION` unchanged. If a later merge bumps it again first, the unreleased version is skipped: Go is fine with the gap, and the next release includes its changes. `publish` warns about each skipped version. It needs `GITHUB_TOKEN`, with `contents: write`, and `GITHUB_REPOSITORY`.

Every request to GitHub, looking up a release or creating one, runs one at a time, in module order. What doesn't touch GitHub runs up to `--jobs` at once, by default `GOMAXPROCS`: the new versions' release notes, read from git, and, once every release is created, their announcements to the proxy. An announcement that panicked, or never ran because the run was interrupted, shows as a proxy warning, like any other proxy failure, though an interrupted run still fails.

### vulncheck

Runs govulncheck, with `GOWORK=off`, over every library module in `go.work` and every directory directly under `tools/` with a `go.mod` (skipping, as the go command does, names starting with `.` or `_` and `testdata`), scanning each module's packages and the tools its `go.mod` pins. It keeps only the vulnerabilities reachable from that code, which is what govulncheck's text output reports as affecting it. It prints a markdown report, one table per affected module listing each advisory's ID, summary, affected dependency, fixed version and link, and writes it to the job summary in GitHub Actions. The report ends with a hidden `<!-- govulncheck-findings: ... -->` marker listing each module's advisories as `module:ID` pairs, with `+fix` on those that have a fixed version, or a `sha256:` digest of them when the list would crowd the issue body.

It then keeps one open issue labelled `govulncheck` in step with the findings:

| Findings | Open issue | Result |
|---|---|---|
| some | none | an issue opens with the report, and the label is created if it's missing |
| some | one | when the findings differ from its marker, a comment lists what's new, what has a fix now, what lost its fix and what's gone, per module; then its body is replaced with the report. A body without a marker is replaced silently |
| none | one | the issue closes, then a comment links the run |
| none | none | nothing |

Finding vulnerabilities doesn't fail the command; the issue is the alert. A scan that fails to run does, before the issue is touched; the job summary still shows what the other modules' scans found, marked incomplete. It needs `GITHUB_TOKEN`, with `issues: write`, and `GITHUB_REPOSITORY`; the closing comment links the run through `GITHUB_SERVER_URL` and `GITHUB_RUN_ID`.

`--govulncheck <path>` names the binary, by default the one on `PATH`. `--dry-run` prints the report and leaves the issue alone, so it needs no token. `--jobs` bounds how many modules and tools are scanned at once, by default `GOMAXPROCS`.

## In CI

The workflows don't `go run` the tool. The local action [.github/actions/release-tool](../../.github/actions/release-tool/action.yaml) restores the tool, and the gorelease and govulncheck its `go.mod` pins with `tool` directives, from the Actions cache, or builds and caches all three on a miss. The cache key hashes `.go-version`, the action itself, this module's `go.mod` and `go.sum`, and its non-test Go files, so a hit is always current. On a hit, `plan` and `publish` skip setting up Go entirely; `release-check` and the weekly scan still set it up, without the dependency cache, because gorelease and govulncheck run the go command. The pull request scan uses the same action for its govulncheck, so every scan runs the one version this module pins. GitHub evicts a cache after 7 days unused.

## Running check locally

```bash
git fetch origin --tags
cd tools/release
GOWORK=off go run . check --base origin/main
```

`check` compares commits, from the merge base to `HEAD`, and gorelease refuses to run on a working tree with uncommitted changes, so commit first. `plan` takes the same `--base` flag.

```bash
cd tools/release
GOWORK=off go run . vulncheck --dry-run
```

scans the working tree as it is, with the govulncheck on `PATH`.

## Testing

```bash
cd tools/release
GOWORK=off go test -short ./...  # skips the tests that build and run gorelease
GOWORK=off go test ./...
```

Tests build throwaway git repositories under `t.TempDir()`, with their own git config. The gorelease package's own tests run shell scripts standing in for gorelease. The gorelease tests also isolate the go command (`GOENV=off`, a private module cache, `GOPROXY=off`) and serve fixture modules from the repository through a git `insteadOf` rule.
