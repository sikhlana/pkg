## Why

Every stateful module still to come takes an injected `*slog.Logger`, so apps need one built the same way, and request-scoped values such as a request id need a way onto every record without each module reading them by hand. go-modules' `logger` builds the logger, but it reads its format from operator config, writes to stdout, drops every record under quiet (errors included), and ties the text format to charm, whose terminal stack reaches anything that imports the module: `ssh` pulls it in just for `NewWriter`. Nothing carries attributes from the context onto records either; `accesslog` and `problem` each call `requestid.From` themselves. It's rebuilt here as `log` rather than copied.

## What Changes

- New module `log` (`github.com/sikhlana/pkg/log`), starting at 0.1.0. It builds the app's `*slog.Logger` from developer wiring only; it has no Config:
  - **Level.** Two flags, debug and quiet, arrive as wiring. Debug logs from Debug up, quiet from Warn up, neither from Info up, and debug wins when both are set. The kernel will map `--debug` and `--quiet` onto them.
  - **Format.** JSON or text, as wiring. The format type decodes from text, so a `--format` flag can parse it and reject anything else before fx starts. The app developer picks the flag's default; an unset format is JSON.
  - **Destination.** Stderr unless the app supplies another writer.
  - **Text handlers are pluggable.** Core takes a text-handler constructor as wiring and falls back to `slog.TextHandler` without one. Two subpackages supply one each: `log/tint` (lmittmann/tint, for services) and `log/charm` (charm.land/log, for operator tools), each exporting `ProvideTextHandler` built on core's helpers. Core never imports either, so charm's tree is built only by apps that import `log/charm`. The charm adapter renders slog groups as qualified keys, which charm's own `WithGroup` doesn't.
  - **Source location.** JSON records always carry it; text records carry it only under debug.
  - **Middleware.** Modules and apps register middleware into an fx group, each with an order the app gives at registration; lower runs first and ties run in whatever order fx gives. A middleware sees each enabled record and passes it on, changed or not, or drops it. Its signature uses only standard-library types, so a sibling module can export one without importing `log`. Core owns the `slog.Handler` contract, so a middleware never handles `WithAttrs` or `WithGroup`, and `logger.With` can't bypass it.
  - **Attributes land at the root.** Core holds back open groups and rebuilds each record relative to the root before the chain runs, so an attribute a middleware adds sits at the top level, not inside a group the caller opened.
  - **Groups with the same key merge.** After the chain, core merges groups that share a key, recursively, including groups passed to `With`. A middleware adding `http.request_id` and accesslog's own `http` group become one `http` object instead of two duplicate keys.
  - **fx events.** An exported constructor for `fx.WithLogger` routes fx's events through the logger at Debug and its failures at Error, so the level decides what shows.
  - **Writer.** An `io.WriteCloser` that logs each line written to it, for subprocess output. Records carry the location that created the writer, lines longer than 64 KiB are split rather than buffered without bound, and the level is checked before a line becomes a string. Each record goes to the logger's handler with the writer's context, so middleware runs on it like any other record. The module also provides a writer provider: a module that runs subprocesses takes it from the graph instead of the logger, and creates writers with a level, a context and typed attributes for every record, each reporting where it was created.
- How it differs from go-modules' `logger`:
  - Renamed `log`; `Verbose` becomes debug.
  - No Config: `Format`, `ErrUnknownFormat` and `Config.Validate` go. Format, level and destination are wiring.
  - Quiet raises the level to Warn instead of discarding every record.
  - Stderr replaces stdout. The `Destination` interface, `DefaultDestination` and redirecting with `fx.Decorate` give way to a tagged writer with `AsDestination` and `WithDestination`.
  - The exported tag constants go, and the flag helpers take a value (`WithDebug(bool)`) instead of variadic bools that default to true.
  - charm moves to an optional subpackage beside tint, behind a pluggable text-handler slot.
  - New: middleware, root placement, group merging, the fx event logger.
  - `Writer` reports where it was created instead of its own flush, checks the level, and caps a line.
  - `logtest` isn't ported yet; it waits for the first consumer, at its new home `testing/logtest`.
- Constitution:
  - **Typed attributes.** Logging calls pass `slog.Attr` values (`slog.String`, `slog.Int`, `slog.Duration`, `slog.GroupAttrs`), never key-value pairs, with `slog.Any` only where no typed constructor fits. sloglint's `attr-only` enforces it, which also steers `slog.Group` to `slog.GroupAttrs` and `Logger.Log` to `LogAttrs`.
  - **Test doubles move to `testing/<module>test`,** each its own module, as the standard library's `testing/slogtest` does. It's an exception to the `<group>/<group><name>` rule. A module's own tests never import its double, since the two go.mod files would require each other.
  - **The logging rule names the standard library's `slog` and `log` package-level functions,** now that the repo has a `log` module of its own.
  - **Decode methods take a pointer receiver on a type whose other methods take values,** as `slog.Level` does, so a constant like `log.FormatText` still satisfies `encoding.TextMarshaler`. recvcheck already exempts them.
- `.golangci.yml` turns on sloglint's `attr-only`.
- Root README gains a Modules row, and its examples that name a `logger` module name `log`. `go.work` registers `./log`.

## Capabilities

### New Capabilities

- `log`: level and format selection, the destination, text-handler plugging and its fallback, source location by format and level, middleware registration and ordering, root placement, group merging, fx event routing, and the line-writer and its provider.

### Modified Capabilities

None. `cli` keeps its contract; the kernel will add the flags.

## Impact

- **`log` (new module):** all of its exported API is new. Its exposure shapes:
  - a singleton `*slog.Logger`;
  - a Provider for the line-writer, `WriterProvider`, which `Module` provides alongside the logger;
  - developer wiring with `AsX` and `WithX` pairs for debug, quiet, format, destination and the text handler;
  - a middleware group, with its constant and registration helpers;
  - plain functions for the fx event logger and the line-writer;
  - in `log/tint` and `log/charm`, `ProvideTextHandler` and the handler constructor for standalone use.

  It has a `Module`, no Config and no runner.
- **`cli`, `config/configvalidator`, `tools/release`:** unchanged.
- **New dependencies (`log` only):**
  - go.uber.org/fx;
  - lmittmann/tint and golang.org/x/term, imported only by `log/tint`;
  - charm.land/log/v2 and its lipgloss tree, imported only by `log/charm`;
  - testify, for tests only.
- **Docs and repo files:**
  - `openspec/config.yaml` (the four constitution edits and the `logger` examples);
  - `.golangci.yml` (`attr-only`);
  - `README.md` (Modules row, examples);
  - `go.work` (`use ./log`);
  - `docs/log.md`, rolled up at archive: why there's no Config, middleware ordering, root placement and group merging.
- **Release:** merging publishes `log/v0.1.0`. It requires no sibling, so it ships in one pull request.
