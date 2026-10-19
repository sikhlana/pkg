# Code layout

[openspec/config.yaml](../openspec/config.yaml)'s Module layout section says where code goes. This page says why. The full discussion is in the archived [httpserver-spec-constructor](../openspec/changes/archive/2026-10-20-httpserver-spec-constructor/design.md) change, where the rule was written and applied to every module.

## One file per idea

A file named for one idea answers "where is X?" before anyone opens it: the Provider is in `provider.go`, the options in `options.go`, the server in `server.go`. The rule came from `http/httpserver`'s `httpserver.go`, which had grown to hold the server, the Provider and its options once the Provider took options. Splitting by idea, rather than by size, keeps a file's contents predictable as it grows; a small idea isn't worth a file, so it may stay with what uses it.

`<package>.go` is the exception that names the package rather than an idea: it holds the entry point (`kernelrun.Run`, `log.New`, `configloader.Load`) or only the package doc. Six packages already put their entry point there, and renaming those files to fit a doc-only rule would have bought nothing.

## Helpers live with their idea

An idea's `ProvideX`, `WithX` and `AsX` go in `x.go` when it exists, beside the type they wire, so changing an idea touches one file. A small idea's helpers stay in `module.go`, which otherwise holds only the `Module` variable and wiring that spans ideas. The first wording put every `AsX`/`WithX` pair in `module.go`, which split `log`'s format from its helpers and left unpaired helpers such as `WithMiddleware` with no clear home.

## Every sentinel in errors.go

Sentinels are a package's documented failure modes, and a caller matching them wants the list in one place. Keeping each with the idea that returns it was tried first, with shared ones in `module.go`; it needed a judgment on every error, and an error had to move the day a second idea returned it, as httpserver's `ErrNilHandler` did. `errors.go` needs no judgment. Error types that only report failures go there too; one that is the subject of its own file, such as cli's `ExitError` in `exit.go`, stays with it.

## Tests beside their file

`x_test.go` tests `x.go`, so a change and its tests sit next to each other and a test file never outlives the code it tested. `helpers_test.go` and `export_test.go` hold shared plumbing, and a test that drives the package through its entry point to check behaviour spanning several files stays in `<package>_test.go`: forcing it into one idea's file would misdescribe what it tests.

## Moves are checked, not trusted

Applying the rule moved code in every module at once, so each move was checked mechanically: every non-import line before and after compared as a multiset, `go doc` output compared, and test counts compared. A move that changed a line showed up as a difference, and the only differences allowed were regrouped declarations and build tags. A refactor of this kind should be checked the same way.
