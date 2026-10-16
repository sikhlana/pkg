## 1. Module scaffold

`log` is new, so it starts at 0.1.0 with no bump task, and no other module changes. It has no mocked interfaces, so there's no go generate task. Nothing is copied from go-modules' `logger`: code, godoc and comments are all written here, from what this code does.

- [x] 1.1 Branch `add-log` off `origin/main` after `git fetch`.
- [x] 1.2 Create `log/`:
  - `go.mod` for `github.com/sikhlana/pkg/log`, with the go directive from `.go-version`;
  - `VERSION` containing `0.1.0`;
  - require the latest go.uber.org/fx, lmittmann/tint, golang.org/x/term, charm.land/log/v2 and stretchr/testify.
  - Done when `go build ./...` passes from `log/`.
- [x] 1.3 Register `./log` in the root `go.work`.
- [x] 1.4 Settle the two fx risks from the design before building on them, each with a test that stays:
  - `fx.As` together with `fx.ResultTags`, for `AsDestination` with a constructor returning `*os.File`; if fx rejects it, take the fallback in design Risks;
  - one anonymous `fx.Module` per `ProvideMiddleware` call with a private name, registered twice in one app without colliding; if fx rejects it, take the counter fallback in design decision 3.

## 2. Wiring and New

- [x] 2.1 `Format` with value receivers for `String` and `MarshalText`, a pointer receiver for `UnmarshalText`, and `ErrUnknownFormat`. See design decision 1.
- [x] 2.2 `TextHandlerProvider`, `Params` and `New`: the level from debug and quiet, standard error by default, JSON with the source always, text through the provider (or `slog.TextHandler`) with the source only under debug, and `ErrUnknownFormat` for a value outside the constants. See design decision 2.
- [x] 2.3 The `AsX` and `WithX` pairs for debug, quiet, format, destination and the text handler, and `Module`. `WithX` takes a value, not a variadic.
- [x] 2.4 Tests for the spec's level, output, format, text-rendering and source-location requirements:
  - each level scenario, including a text provider that writes everything it's given;
  - the standard-error default, in a re-run of the test binary that captures its standard error;
  - a supplied writer;
  - decoding `text` and `yaml`, the round trip, and a converted out-of-range value;
  - the fallback's plain `key=value` line, and a JSON app never calling its text provider;
  - the source in JSON, the flag handed to the text provider with and without debug, and a record created without one.
  - Run the real `Module` under `fxtest.New` with the `WithX` helpers for the fx cases.

## 3. The core handler and middleware

- [x] 3.1 The core handler: `Enabled` checks the level before the base's; `WithAttrs` forwards plain attributes until the first group and holds groups and group-valued attributes; `WithGroup("")` returns the handler; `Handle` rebuilds the record relative to the root only when something is held. See design decision 4.
- [x] 3.2 `Middleware`, `OrderedMiddleware` and `MiddlewareGroup`. `New` sorts the middleware stably by order, and each handler value builds its chain once, not per record. See design decision 3.
- [x] 3.3 The merge after the chain: same-key top-level groups, recursively, at the first one's position, with no allocation when there's no duplicate. See design decision 5.
- [x] 3.4 `WithMiddleware` and `ProvideMiddleware`, with the outcome of 1.4.
- [x] 3.5 Tests, with a private recording handler as the base:
  - `testing/slogtest` over a JSON base with middleware registered;
  - the spec's middleware scenarios: the context, a derived logger, a record handed to the handler, dropping, below the level;
  - ordering: a higher order sees a lower one's attribute, a lower drop hides the record, and a constructor at 15 between values at 10 and 20;
  - root placement inside an open group, and with attributes before and after a group;
  - merging: a middleware's group with the caller's, nested groups, a group attached with `With`, and plain duplicates kept.

## 4. fx event logger

- [x] 4.1 `NewEventLogger`: an `fxevent.SlogLogger` at Debug, failures at Error. See design decision 7.
- [x] 4.2 Tests with `fx.WithLogger(log.NewEventLogger)` and the real `Module`: no fx records by default, provide, invoke and hook records under debug, and a failing start hook's Error record under quiet.

## 5. Writer

- [x] 5.1 `Writer` and `NewWriter`: the caller's PC captured once; `Enabled`, then a record handed to the logger's handler with the writer's context; `\r\n` stripped; empty lines skipped; a 64 KiB run cut after the last complete UTF-8 character; `Close` flushing the rest; a mutex on every method; `Write` succeeding whatever the handler returns. See design decision 8.
- [x] 5.2 Tests for the spec's two line-writer requirements: two lines, carriage returns and empty lines, a partial line on close, 100 KiB of multi-byte characters, a disabled level, the source at the test's `NewWriter` line, a middleware reading the writer's context, a handler that returns an error, and concurrent writes.

## 6. Text handlers

- [x] 6.1 `log/tint`: `NewHandler` passing the level, source flag and `ReplaceAttr` to tint, with colour only on a terminal `*os.File` and an unset or empty `NO_COLOR`, and `ProvideTextHandler`. It imports `log`; `log` never imports it. See design decision 6.
- [x] 6.2 `log/tint` tests: no colour codes to a buffer, none with `NO_COLOR` set, the level and source honoured, and an fx app with `ProvideTextHandler` and the text format writing through it.
- [x] 6.3 `log/charm`: `NewHandler` wrapping a charm logger in the adapter that tracks groups, flattens to dot-qualified keys, resolves `LogValuer`s, inlines empty-key groups, drops empty attributes and keeps the record's time, level, message and PC; and `ProvideTextHandler`. See design decision 6.
- [x] 6.4 `log/charm` tests: `http.status=200` with no `http` prefix on the message, the level and source honoured, and `testing/slogtest` over charm's logfmt formatter with a parse function that nests dot-qualified keys.

## 7. Constitution, lint and docs

- [x] 7.1 In `openspec/config.yaml`:
  - Code standards: log with typed attributes (`slog.String`, `slog.Int`, `slog.Duration`, `slog.GroupAttrs`), with `slog.Any` only where no typed constructor fits; Tooling: sloglint's `attr-only` enforces it;
  - the logging rule names the standard library's `slog` and `log` package-level functions;
  - the receiver rule's exception for decode methods (`UnmarshalText` and the like), which take a pointer receiver on a type whose other methods take values, as `slog.Level` does;
  - consumer-facing doubles at `testing/<module>test`, each its own module, as an exception to `<group>/<group><name>`, and a module's own tests never importing its double. Update every place that names `<module>test/`: the fx integration exemption, Testing, the module layout diagram, the Documentation section's Test Helpers line and the Specs line;
  - examples that name a `logger` module name `log`.
- [x] 7.2 In `.golangci.yml`, set sloglint's `attr-only: true`.
- [x] 7.3 Write `log/README.md` in the constitution's order:
  - overview;
  - Configuration: none, and say why;
  - Exported Members: every export of `log`, `log/tint` and `log/charm`, qualified, with the `Format` methods and the `OrderedMiddleware` fields;
  - Usage, Standalone: `New` with `Params` filled by hand, a text handler from a subpackage, a middleware, and `NewWriter` for a subprocess;
  - Usage, With fx: `Module` with the `WithX` helpers, `ProvideTextHandler`, both middleware helpers, and `fx.WithLogger(log.NewEventLogger)`;
  - attributes: always typed, the `*Context` methods so middleware sees the context, and groups merging under Datadog's standard names;
  - middleware: lower orders run first, the suggested bands, equal orders unspecified, and cloning a record before keeping it.
  - No backticks in table cells.
- [x] 7.4 In the root `README.md`, add the `log` row to the Modules table in alphabetical order, and make the examples that name a `logger` module name `log`.

## 8. Verification

- [x] 8.1 In `log/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 8.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 8.1 and 8.2.

## 9. Code review fixes

The review found 14 issues and reproduced five of them. Design decisions 2, 3, 4, 5, 6 and 8 and the spec record the outcome.

- [x] 9.1 The core handler holds every attribute given to `WithAttrs`, in order, and forwards none to the base. A redactor now sees attributes attached with `With`, the caller's order is kept, and the chain is built once in `New` instead of on every `With`. Tests: a redactor over a `With` attribute; plain and grouped attributes keeping their order; `WithAttrs` allocating the same with five middleware as with none; the recording base failing the test if attributes reach it.
- [x] 9.2 Groups that share a key merge at any depth, not only at the top, so a `With` group and the record's group of the same key merge inside an open group. The duplicate check still allocates nothing. Test: one `http` object under `req` in the JSON output.
- [x] 9.3 `WithDestination(nil)` supplies nothing instead of panicking, and `New` treats a nil pointer behind the writer as standard error. Test: an untyped nil and a nil `*os.File` through fx, and a nil `*bytes.Buffer` in `Params`, all reach standard error in a child process.
- [x] 9.4 `Writer` applies the 64 KiB cap to every line, however the writes arrive, and logs complete lines straight from `p`, buffering only a trailing partial line. Tests: a 100 KiB line gives the same records in one write, 32 KiB writes and 1000-byte writes; the buffer stays empty for complete lines and at most two runs' capacity for a 1 MiB run.
- [x] 9.5 `log/charm` labels levels between the standard ones as slog names them, from Debug-8 to Error+8, and with the nearest standard label beyond, set before the logger is shared. Charm's `FATAL` never appears. Test: Debug-4, Info+2, Error+4, Error+12 and Debug-12, one line each.
- [x] 9.6 `log/tint` reaches the descriptor through `SyscallConn`, so the terminal check doesn't switch a non-blocking file to blocking mode. Unix test: a pipe stays non-blocking after `tint.New`, and `Fd` is shown to clear the flag.
- [x] 9.7 `tint.NewHandler` and `charm.NewHandler` become `tint.New` and `charm.New`.
- [x] 9.8 `ProvideMiddleware`'s adapter takes a params struct embedding `fx.In`; `NewEventLogger` drops the redundant `UseErrorLevel`; `fold` and `merge` share a `recordAttrs` helper.
- [x] 9.9 The README's file example opens the file before the app and closes it after `Stop` instead of in a stop hook, and the README follows the renames and new behaviour.
- [x] 9.10 Repeat 8.1 and 8.2: lint reports 0 issues, `go test ./...` and `go test -race ./...` pass in `log/`, and the `-short` sweep passes across all three modules.

## 10. Writer provider

Added after review; `log` is unreleased, so VERSION stays 0.1.0. See design decision 8.

- [x] 10.1 `WriterProvider`, `WriterProviderParams` and `NewWriterProvider`, with `Module` providing it next to `New`. Its writers attach the given attributes with `Handler().WithAttrs`, so `Writer` holds a handler rather than a logger, and the closure captures its caller's PC through the helper `NewWriter` shares.
- [x] 10.2 Tests: a provider from the real `Module` writing at Warn with a `stream` attribute and a context a middleware reads, with the source at the provider call; and two writers from one provider keeping their attributes apart.
- [x] 10.3 README: the four new exports, the `Module` row, and an fx example taking the provider through a `Params` struct.
- [x] 10.4 Repeat 8.1 and 8.2.
