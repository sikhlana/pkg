## Context

The source is `logger` in idio/go-modules (0.5.0 there): a `Config` with a `Format` string, a `Destination` interface replaced with `fx.Decorate`, quiet and verbose flags under exported tag constants, charm as the only text handler, a line-writer and a JSON spy. See proposal.md for why it's rebuilt as `log` rather than copied; the requirements are in specs/log/spec.md. This document covers how they're met and what was rejected.

Facts that shape the approach, checked against sources:

- **slog never merges groups, and its JSON handler writes duplicate keys.** Two `http` groups on one record print as two `"http"` keys, and most JSON parsers keep the last.
- **Attributes added to a record land in the innermost open group.** After `logger.WithGroup("http")`, a handler that calls `r.AddAttrs` puts the attribute under `http`.
- **`slog.Logger.Log` and `LogAttrs` always record their own caller.** There's no public way to hand them a PC; a caller that wants another source builds the record and calls `Handler().Handle`, as go-modules' `accesslog` does.
- **dig shuffles value groups on purpose** (`shuffledCopy` in dig's scope.go), so a group's order differs from run to run.
- **charm's slog adapter turns `WithGroup` into the message prefix** (`WithGroup` calls `WithPrefix`), so grouped keys lose their qualification. It skips the caller when a record's PC is zero.
- **tint has no dependencies and doesn't detect terminals;** the caller decides `NoColor`.
- **`fx.WithLogger` inside an `fx.Module` only covers that module's events,** so the app-wide event logger has to be wired at the top level.
- **pflag v1.0.9, which `cli` already requires, has `TextVar`,** so a flag can parse any `encoding.TextUnmarshaler`.
- **recvcheck exempts `UnmarshalText` and the other decode methods by default,** since they need pointer receivers.
- **In go-modules,** `ssh` imports `logger` only for `NewWriter`, nothing calls `WithGroup`, `accesslog` and `problem` read the request id by hand, and every test that captures logs builds its own JSON handler over a buffer.

## Goals / Non-Goals

**Goals:**

- One way to build the app's `*slog.Logger`, with or without fx, and nothing an operator configures.
- A middleware contract a sibling module can satisfy without importing `log`.
- Attribute placement that doesn't depend on whether the caller opened a group.
- Text handlers whose dependencies are built only by the apps that pick them.

**Non-Goals:**

- Flags. The kernel adds `--debug`, `--quiet` and `--format` and maps them onto this module's wiring.
- Choosing tint as the default text handler. The kernel does; core falls back to the standard library.
- Context-carried attributes for app code (`log.ContextWith(ctx, …)`). It's one built-in middleware away and a follow-up.
- Changing the level at runtime, extra sinks fanned out with `slog.NewMultiHandler`, and calling `slog.SetDefault`. The last is the kernel's call (`fx.Invoke(slog.SetDefault)`).
- `testing/logtest`. It waits for its first consumer.

## Decisions

### 1. The shape of the package

```go
package log

type Format int

const (
	FormatJSON Format = iota // the zero value: unset means JSON
	FormatText
)

var ErrUnknownFormat = errors.New("unknown log format")

func (f Format) String() string
func (f Format) MarshalText() ([]byte, error)
func (f *Format) UnmarshalText(text []byte) error

type TextHandlerProvider func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

type Middleware func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error

type OrderedMiddleware struct {
	Order      int
	Middleware Middleware
}

const MiddlewareGroup = `group:"log:middleware"`

type Params struct {
	fx.In

	Debug       bool                `name:"log:debug"        optional:"true"`
	Quiet       bool                `name:"log:quiet"        optional:"true"`
	Format      Format              `name:"log:format"       optional:"true"`
	Destination io.Writer           `name:"log:destination"  optional:"true"`
	TextHandler TextHandlerProvider `name:"log:text_handler" optional:"true"`
	Middleware  []OrderedMiddleware `group:"log:middleware"`
}

func New(p Params) (*slog.Logger, error)

func AsDebug(v any) any                  // and WithDebug(bool) fx.Option
func AsQuiet(v any) any                  // and WithQuiet(bool) fx.Option
func AsFormat(v any) any                 // and WithFormat(Format) fx.Option
func AsDestination(v any) any            // and WithDestination(io.Writer) fx.Option
func AsTextHandler(v any) any            // and WithTextHandler(TextHandlerProvider) fx.Option
func ProvideMiddleware(order int, constructor any) fx.Option
func WithMiddleware(order int, m Middleware) fx.Option

func NewEventLogger(logger *slog.Logger) fxevent.Logger

func NewWriter(ctx context.Context, level slog.Level, logger *slog.Logger) *Writer

type WriterProvider func(ctx context.Context, level slog.Level, attrs ...slog.Attr) *Writer

type WriterProviderParams struct {
	fx.In

	Logger *slog.Logger
}

func NewWriterProvider(p WriterProviderParams) WriterProvider

var Module = fx.Module("log", fx.Provide(New, NewWriterProvider))
```

- **Every wiring value is name-tagged and optional,** even `Format` and `TextHandlerProvider`, whose types are distinct. The tag gives `AsX` something to do and keeps an untagged value elsewhere in the graph from being picked up. Following configloader, there are no exported name-tag constants; the `AsX` and `WithX` pairs cover every way to supply one. The group tag is exported, as the constitution asks for groups.
- **`Format` is an int whose zero value is JSON,** so "unset means JSON" needs no `Default*` constant or resolver, and the `exhaustive` linter checks every switch on it. `UnmarshalText` accepts `json` and `text` and returns `ErrUnknownFormat` for anything else, so the kernel's flag is `fs.TextVar(&format, "format", log.FormatText, …)` and a bad value fails while cobra parses. `New` returns `ErrUnknownFormat` too, for a value outside the constants, which only a conversion can produce.
- **`Format` takes a pointer receiver for `UnmarshalText` and value receivers for the rest,** as `slog.Level` does: `log.FormatText` has to satisfy `encoding.TextMarshaler` as a value for `TextVar`'s default. That mixes receivers, so the constitution's receiver rule gets an exception for decode methods, the same set recvcheck already exempts. The alternative, pointer receivers throughout, makes a `Format` value print as an int and forces `TextVar` callers to pass a pointer as the default.
- **`TextHandlerProvider` follows the constitution's Provider shape:** a function type that builds a handler from its options. Its signature is standard-library only.
- **`NewEventLogger` and `NewWriter` take plain parameters.** Neither is registered with `fx.Provide`; the first goes to `fx.WithLogger`, which resolves `*slog.Logger` from the graph.

### 2. How New builds the logger

```
level:   Info, Warn if quiet, Debug if debug (debug wins)
writer:  Destination, or os.Stderr when it's nil or a nil pointer
base:    FormatJSON → slog.NewJSONHandler(w, {Level, AddSource: true})
         FormatText → TextHandler(w, {Level, AddSource: debug}), or slog.NewTextHandler without one
logger:  slog.New(core handler{level, base, middleware sorted by Order})
```

- **Quiet is Warn, not a discard handler.** go-modules returned `slog.DiscardHandler`, which dropped errors with everything else.
- **A nil destination means standard error,** and so does a nil pointer, map, slice, channel or function of a writer type, found with reflection: a `(*os.File)(nil)` would fail every write and lose every record, and a nil `*bytes.Buffer` would panic. `WithDestination` supplies nothing for either, since `fx.Supply` panics on an untyped nil.
- **Text shows the source only under debug.** An operator tool's normal output stays clean; JSON always carries it for the log pipeline.
- **The core handler enforces the level in `Enabled` itself,** before asking the base. A text handler that ignores `opts.Level` can't leak records below it.
- **Middleware is sorted once in `New`,** stably by `Order`. Equal orders keep whatever order dig gave, which changes between runs; the README says so.

### 3. Middleware is a record step, registered with an order (→ docs/log.md)

A middleware receives each enabled record with the caller's context and calls `next` to pass it on, changed or not; not calling it drops the record.

- **Why not `func(slog.Handler) slog.Handler`:** every such middleware has to re-wrap itself in `WithAttrs` and `WithGroup`, and one that forgets makes every `logger.With(...)` skip it silently. `With` is everywhere in go-modules (`http/server`, `http/client`, `accesslog`). The core handler owns the `slog.Handler` contract once, and middleware never sees it.
- **Why not attribute extractors (`func(ctx) []slog.Attr`):** they can't drop or rewrite a record, so samplers and redactors would need a second mechanism. A record step covers attribute injection as well.
- **Why the signature is standard-library only:** `next` is an unnamed function type, so a sibling module can export a matching function, satisfying the `Middleware` type by assignability, without importing `log`. The app registers it with `log.WithMiddleware(order, requestid.LogAttrs)`.
- **Why the order is given at registration, not by a method:** modules can't see each other's numbers or any constant in `log` without importing it, so an `Order()` method means picking numbers blind. The app sees the whole composition. Lower runs first, nearest the caller; the base handler is last. The README suggests bands: samplers low, injectors in the middle, redactors high.
- **Ties are left to fx.** Breaking them by name was considered and rejected as hiding a choice the app should make with distinct orders.
- **Two registration helpers.** fx can't tell a function value from a constructor, so `WithMiddleware` supplies a `Middleware` value and `ProvideMiddleware` registers a constructor that builds one from the graph. To attach the order to a constructor of any signature, `ProvideMiddleware` returns an anonymous `fx.Module` that provides the constructor's result privately under a fixed name and an adapter, taking it through a params struct like every `fx.Provide` constructor, that pairs it with the order into the group. The private name is scoped to that module, so registrations don't collide. If fx rejects that, the fallback is a unique name per registration from a package counter.
- **Record copies.** Copies of a `slog.Record` share their attribute storage. The chain passes one record down linearly; a middleware that keeps a record or passes it on more than once clones it first, and the README says so.

### 4. Attributes a middleware adds land at the root (→ docs/log.md)

The core handler holds every attribute given to `WithAttrs` and every group given to `WithGroup`, and rebuilds each record relative to the root before the chain runs:

```
logger.With(slog.String("server", "api")).WithGroup("http").InfoContext(ctx, "served", slog.Int("status", 200))

held     = [server=api], [group "http"]                    nothing reaches the base
record   = {msg: served, server: api, http: {status: 200}}  rebuilt relative to the root
chain    adds request_id                                    → top level
out      {"msg":"served","server":"api","http":{"status":200},"request_id":"…"}
```

- **Nothing given to `WithAttrs` or `WithGroup` reaches the base.** The middleware sees the whole record: a redactor sees an attribute attached with `With` as it sees one passed to the call. The caller's attributes keep their order, and the base never changes after `New`, so the chain is built once.
- **`WithGroup("")` returns the handler unchanged,** as slog's handler rules require.
- **With nothing held, the record passes through untouched.** A logger with `With` attributes or an open group rebuilds every record, and the base encodes those attributes per record rather than once.
- **Rejected in review: forwarding plain attributes to the base until the first group,** so JSON could encode them once. Middleware never saw them, so a redactor couldn't act on a secret attached with `With`, and splitting plain attributes from held groups reordered the caller's attributes.
- **The fold is the part veqryn/slog-context does,** and with forwarding gone it now rebuilds records the same way. It stays rejected because its extractors can't drop records, and adopting it would still leave ordering and merging to write.

### 5. Groups with the same key merge (→ docs/log.md)

After the chain and before the base, the core handler merges group-valued attributes that share a key with a sibling, at any depth:

- the merged group sits where its first occurrence was, and attributes keep their order;
- attributes passed to `With` are held (decision 4), so their groups merge too, including inside a group opened with `WithGroup`, which the fold nests below the top level;
- two plain attributes with the same key stay two keys, as slog leaves them; picking one silently would hide a bug;
- a `LogValuer` and an empty-key group are passed through as they are, since resolving them early would resolve them twice;
- the check walks the record's groups once per record without allocating, and rebuilds the record only when it finds a duplicate.

It exists for Datadog's grouped standard attributes: `accesslog` writes `http.method` and `http.status_code`, and a request-id middleware adds `http.request_id`. Contributors use the standard names and get one `http` object.

### 6. Text handlers live in subpackages that import core

```
log/          core: never imports tint/ or charm/
├── tint/     New, ProvideTextHandler → log.WithTextHandler(New)
└── charm/    New, ProvideTextHandler → log.WithTextHandler(New)
```

- **Dependencies point inward,** so there's no cycle, and charm's tree is built only by apps that import `log/charm`. Both stay in `log`'s go.mod, as the constitution's subpackage rule has it.
- **`New` matches `TextHandlerProvider`,** so standalone callers put it in `Params.TextHandler`. Each package has one principal type, so the constructor is `New`, not `NewHandler`. `ProvideTextHandler` takes no options in 0.1.0.
- **`log/tint`** builds `tint.NewTextHandler` with the options' level, source flag and `ReplaceAttr`. Colour is on only when the writer is an `*os.File` that's a terminal (golang.org/x/term) and `NO_COLOR` is unset. The descriptor is reached through `SyscallConn`, since `os.File.Fd` switches a non-blocking file, such as a pipe, to blocking mode and stops its deadlines working. Its time format is tint's default, `time.StampMilli`.
- **`log/charm`** builds a charm logger with timestamps on, the level and `ReportCaller` from the options, and wraps it in an adapter that does what charm's own adapter doesn't. It tracks the open groups, flattens attributes into dot-qualified keys (`http.status=200`) as `slog.TextHandler` does, resolves `LogValuer`s, inlines empty-key groups and drops empty attributes. It then passes the flattened record, with its time, level, message and PC, to charm's handler. Colour detection is charm's own, which honours `NO_COLOR`.
- **Charm only prints a level it has a style for,** so a level between the standard ones lost its label and slog's Error+4 printed as charm's `FATAL`. `New` gives charm a style for every level from Debug-8 to Error+8: slog's name for it (`DEBUG-4`, `INFO+2`, `ERROR+4`), in the colour of the standard level below, without the width-5 padding that would wrap a longer label. Further out, a record takes the nearest standard level. The styles are all set before the logger is shared, because charm reads its styles pointer outside its lock, so adding one per new level at runtime would race.
- **Without a provider, text is `slog.TextHandler`:** plain, no colour, no dependencies. The kernel wires tint by default.

### 7. fx events go through the logger at Debug

`NewEventLogger` returns an `fxevent.SlogLogger` over the logger with `UseLogLevel(slog.LevelDebug)`; failures keep its default, Error.

| Mode | provide, invoke and hook events | failures |
|---|---|---|
| debug | shown | shown |
| default | hidden | shown |
| quiet | hidden | shown |

go-modules' kernel chose between `SlogLogger` and `fx.NopLogger` on its verbose flag, which hid fx's failures outside verbose mode. Here the level decides, and failures always show.

### 8. Writer reports where it was created

- **Source:** `NewWriter` captures its caller's PC once with `runtime.Callers`. For ssh that's the line that set up the stdout writer. The caller of `Write` would be `io.Copy` inside os/exec's copying goroutine, which says nothing.
- **Records are built directly:** `Enabled`, then `slog.NewRecord(time.Now(), level, line, pc)`, then `logger.Handler().Handle(ctx, r)`. `Logger.Log` can't take a PC. The middleware lives in the handler, so it still runs, with the writer's context.
- **Considered: carrying the PC in the context and having a built-in middleware set it.** Rejected because it works only under the core handler (a bare `slog.New(slog.NewJSONHandler(…))` would report `writer.go` again), costs every record in the app a context lookup, and throws away the caller lookup `Logger.Log` does anyway.
- **Line cap:** every run of 64 KiB without a newline becomes a record, cut after its last complete UTF-8 sequence, whether it arrives in one write or many, so a split never cuts a rune and never depends on how the producer chunks its output. Complete lines are logged straight from the bytes written; only a trailing partial line is buffered, so the buffer never holds more than one run, whatever the size of a single write.
- **WriterProvider, added after review:** `Module` provides a `WriterProvider`, so a module that runs subprocesses takes it from the graph instead of the logger. It's the constitution's Provider shape: a function type that builds the same kind of value per call, from a constructor taking a params struct. fx builds it only when something asks for one.
  - **Variadic typed attributes** go on every record from the writer, as go-modules' ssh tagged its writers with `stream=stdout`. Binding the graph's logger would otherwise leave no way to do that. They're attached with `Handler().WithAttrs`, so `Writer` holds a handler rather than a logger, and they never pass through `...any`.
  - **The closure captures its own caller's PC,** through the same unexported helper `NewWriter` uses. Calling `NewWriter` from the closure would make every provided writer report a line in `log` itself.
- **Kept from go-modules:** `\r\n` is stripped, empty lines are skipped, `Close` flushes the rest, and every method holds a mutex. `Write` reports success whatever the handler returns, so a failing log destination never breaks the subprocess writing to it.

### 9. Typed attributes, enforced

sloglint's `attr-only` reports key-value pairs in logging calls and in `With`, `slog.Group` (use `slog.GroupAttrs`) and `slog.Log` or `Logger.Log` (use `LogAttrs`). Nothing in the repo logs yet, so it lands clean. The constitution gains the rule under code standards and the ban under tooling. `key-naming-case: snake` was considered and left for later.

### 10. Test doubles move to testing/

Consumer-facing doubles live at `testing/<module>test`, each its own module, as the standard library's `testing/slogtest` does. One `testing/` module would drag every double's dependencies together. The name is an exception to `<group>/<group><name>`; the package name still equals the directory, so `logtest` is its fx name, spec folder and commit scope. A module's own tests never import its double, since each go.mod would require the other and neither could release first. `log` keeps a private recorder in its test files.

### 11. Dependencies

| Dependency | Where | Why it beats hand-rolling |
|---|---|---|
| go.uber.org/fx | core | the constitution's integration layer; `fxevent.SlogLogger` for decision 7 |
| lmittmann/tint | `log/tint` | a coloured handler that follows slog's handler rules is fiddly; tint has no dependencies and about 1,070 importers |
| golang.org/x/term | `log/tint` | the Go project's own terminal check |
| golang.org/x/sys | `log/tint` tests | reads a descriptor's flags, to check that the terminal check leaves a pipe non-blocking; already in the tree through x/term |
| charm.land/log/v2 | `log/charm` | the charm look and its lipgloss styles, which future charm-based modules share; heavy, so behind the subpackage |
| testify | tests | the constitution's assertion library |

The core handler itself is hand-rolled: about a hundred lines for the fold, the chain and the merge, checked by `testing/slogtest`.

### 12. Testing

- The core handler runs `testing/slogtest` over a JSON base, plus tests for the chain, ordering, dropping, root placement and merging, using a private recording handler as the base.
- The charm adapter runs `testing/slogtest` over charm's logfmt formatter, with a parse function that nests dot-qualified keys.
- `log/tint` tests check that the level, source flag and colour decision reach tint; tint's rendering is its own.
- fx tests run the real `Module` under `fxtest.New` with the `WithX` helpers, including middleware from both registration helpers.
- `Writer` tests cover line splitting, `\r\n`, empty lines, the cap and rune boundaries, `Close`, the level check, the context reaching the middleware, and the source pointing at the test's `NewWriter` line.
- The event logger is tested in each mode from decision 7's table.

## Risks / Trade-offs

- [Equal orders run in a different order each run] → The README says to give distinct orders where it matters.
- [Every record from a logger with `With` attributes or an open group is rebuilt, and those attributes are encoded per record instead of once] → It's what lets middleware see the whole record. Loggers without them pass records through untouched.
- [Merging skips `LogValuer`s and empty-key groups] → Documented; contributors pass groups directly.
- [A middleware keeps a record without cloning it] → Documented on the `Middleware` type.
- [Debug and quiet both set: debug wins silently] → The kernel's flag helper makes the flags mutually exclusive.
- [A charm bump means a `log` patch release for apps that only use tint] → Accepted; `log/charm` can become its own module if the churn gets annoying.
- [`AsDestination` with a constructor returning a concrete writer needs `fx.As` together with `fx.ResultTags`] → Verify in the first task. If fx rejects the combination, `AsDestination` documents values only and `WithDestination` provides through a function returning `io.Writer`.
- [tint colours on Windows] → Not a target; tint's README points at go-colorable for anyone who needs it.

## Migration Plan

`log` is new, requires no sibling, and ships in one pull request. Merging publishes `log/v0.1.0`. go-modules is unaffected. The kernel, a later change, adds the flags, wires `fx.WithLogger(log.NewEventLogger)` and picks tint as the default text handler.

## Open Questions

- Charm's time format and styles, and whether tint's should drop the date. Both are cosmetic and can change in a patch.

## What actually happened

Deviations from the design as first written:

- **The code review reshaped decisions 2, 3, 4, 5, 6 and 8 before the first release** (next section). The decisions above describe what ships. The biggest change: decision 4 no longer forwards plain `With` attributes to the base, so middleware sees the whole record and every such record is rebuilt.
- **Both fx risks resolved without their fallbacks** (task 1.4). fx accepts `fx.As` with `fx.ResultTags`, so `AsDestination` takes a constructor returning `*os.File`. One private `fx.Module` per `ProvideMiddleware` call registers twice in one app without colliding. A test for each stays.
- **`AsTextHandler` under `fx.Supply` needs a value already of the `TextHandlerProvider` type,** since fx takes a function with the provider's signature for a constructor. `WithTextHandler` converts for the caller, and `AsTextHandler`'s godoc says so.
- **`WriterProvider` wasn't in the original proposal.** It was added after review: `Module` provides it, its writers take variadic typed attributes, and its records carry the provider call's location (decision 8).
- **`log/charm` checks the level itself on every record and passes everything to charm,** so a `slog.LevelVar` in the options keeps working.

Findings:

- **charm reads its styles pointer without its lock,** so styles can't be swapped while records are written. Every level label is set when the handler is created.
- **`os.File.Fd` switches a non-blocking file to blocking mode** and stops its deadlines working. A terminal check goes through `SyscallConn` instead.
- **charm pads level labels to width 5, and lipgloss wraps longer text,** so a longer label needs its width unset.
- **`testing.AllocsPerRun` returns a float,** which testifylint wants compared with `InDelta`.

Follow-ups left open:

- **The kernel:** `--debug`, `--quiet` (mutually exclusive) and `--format` flags mapped onto the `WithX` helpers, `fx.WithLogger(log.NewEventLogger)`, tint as the default text handler, and possibly `fx.Invoke(slog.SetDefault)`.
- **Context-carried attributes** for app code, as one built-in middleware.
- **`testing/logtest`,** when the first consumer needs a log double.
- **Changing the level at runtime,** and extra sinks through `slog.NewMultiHandler`.
- **sloglint's `key-naming-case: snake`,** considered and left for later.
- **`log/charm` as a module of its own,** if charm bumps make `log` release too often.
- **Release:** merging publishes `log/v0.1.0`; checking the published release, the proxy and pkg.go.dev happens after merge.

## Fixes from code review

The review of the first implementation found fourteen issues, five of them reproduced. All were fixed before the first release (task section 9):

- **The core handler.**
  - Plain `With` attributes went straight to the base, so no middleware saw them: a redactor missed a secret attached with `With`. → Every `With` attribute is held and folded into the record (decision 4).
  - Splitting plain attributes from held groups reordered the caller's attributes, against the spec. → Held in order.
  - Every `With` rebuilt the middleware chain. → Built once, since the base never changes.
  - Same-key groups merged only at the top level, so duplicates inside an open group survived. → Merged at any depth, still allocation-free without duplicates (decision 5).
- **The destination.** `WithDestination(nil)` panicked inside `fx.Supply`, and a nil `*os.File` silently lost every record. → Both mean standard error (decision 2).
- **Writer.**
  - The 64 KiB cap applied only to bytes left after newline splitting, so a long line written at once became one record of any size. → Every run is capped, however the writes arrive (decision 8).
  - Every write was copied into the buffer, which kept the capacity of the largest one. → Complete lines are logged straight from the bytes written.
- **Text handlers.**
  - charm dropped the label of a level between the standard ones and printed slog's Error+4 as `FATAL`. → Labelled as slog names it (decision 6).
  - tint's terminal check called `Fd`, putting a non-blocking destination into blocking mode. → `SyscallConn`.
- **Conventions and docs.**
  - `tint.NewHandler` and `charm.NewHandler` became `New`, since each package has one principal type.
  - `ProvideMiddleware`'s adapter took a bare parameter; it takes a params struct.
  - `NewEventLogger` dropped an `UseErrorLevel` call that restated fxevent's default.
  - A `recordAttrs` helper replaced three copies of one loop.
  - The README closed a log file in a stop hook, which runs last, losing fx's final events. It now closes after `Stop` returns.

## Rollup at archive

- **Constitution:** four edits were written in this change: typed attributes with sloglint's `attr-only`, consumer-facing doubles at `testing/<module>test`, the logging rule naming the standard library, and the receiver exception for decode methods.
- **docs/log.md** (new): why there's no Config, middleware as record steps ordered at registration, why middleware sees the whole record, group merging, and the text-handler subpackages.
- **The rest** (the PC capture, the writer's cap and buffering, the event logger's levels) is specific to `log` and lives in its spec, README and this archive.
