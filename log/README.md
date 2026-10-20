# log

Builds the app's `*slog.Logger`: JSON or text on standard error or a writer of your choice, a level from debug and quiet switches, pluggable text rendering, and middleware that can enrich, rewrite or drop every record. Includes an fx event logger and a writer that logs a subprocess's output line by line.

## Configuration

None. The level, format and destination are developer wiring: the program sets them in code or maps its own flags onto them (`--debug`, `--quiet`, `--format`). The logger also has to exist before any config is loaded, to report what goes wrong while loading it.

## Exported Members

| Name | Description |
|------|-------------|
| New | Builds the logger from Params. Returns ErrUnknownFormat for a Format other than FormatJSON or FormatText. |
| Params | New's inputs, all optional. With fx, each comes from its WithX helper, or AsX for a value built in the graph. |
| Params.Debug | Logs from Debug up and asks the text handler for the source location. Wins over Quiet. |
| Params.Quiet | Logs from Warn up. Neither switch logs from Info up. |
| Params.Format | JSON or text output. The zero value is FormatJSON. |
| Params.Destination | Where records are written. Standard error when nil, or a nil pointer of a writer type. |
| Params.TextHandler | Renders FormatText records. Without one, slog's text handler renders them, without colour. Never called for FormatJSON. |
| Params.Middleware | The middleware chain, run lowest order first. A nil Middleware, a nil pointer, func, map, slice or channel behind one, and either wrapped by Ordered, is left out. |
| Format | How records are written: FormatJSON or FormatText. Decodes from its name, so a flag can parse it. |
| FormatJSON | One JSON object per line, always with the source location. The zero Format. |
| FormatText | Rendered by the text handler, with the source location only under Debug. |
| Format.String | Returns json, text, or Format(n) for any other value. |
| Format.MarshalText | Returns json or text. Any other value fails with ErrUnknownFormat. |
| Format.UnmarshalText | Sets the format from json or text. Any other name fails with ErrUnknownFormat and leaves the format unchanged. |
| ErrUnknownFormat | Returned for a Format other than FormatJSON or FormatText, by decoding a name or by New. |
| TextHandlerProvider | Builds the text handler from the destination and the handler options. tint.New and charm.New are two. |
| Middleware | A step every enabled record passes through: Handle(ctx, r, next), with the context the record was logged with. Calls next to pass the record on, or returns without calling it to drop it. next is built once, in New, the same for every record. |
| OrderedMiddleware | A Middleware with Order() int. Lower orders run first, nearest the caller. A Middleware without it counts as 0. Equal orders run in an unspecified relative order. |
| MiddlewareFunc | Adapts a function to a Middleware, at order 0. |
| MiddlewareFunc.Handle | Calls the function. |
| Ordered | Returns a Middleware at a given order, replacing any order it declares. |
| MiddlewareGroup | The fx group tag New collects Middleware from. |
| Module | Provides the *slog.Logger, built by New, and a WriterProvider whose writers log through it. |
| AsDebug | Annotates a bool, or a constructor returning one, as Params.Debug. |
| WithDebug | Supplies Params.Debug. |
| AsQuiet | Annotates a bool, or a constructor returning one, as Params.Quiet. |
| WithQuiet | Supplies Params.Quiet. |
| AsFormat | Annotates a Format, or a constructor returning one, as Params.Format. |
| WithFormat | Supplies Params.Format. |
| AsDestination | Annotates a writer, or a constructor returning one of any concrete writer type, as Params.Destination. |
| WithDestination | Supplies Params.Destination. A nil writer, or a nil pointer of a writer type, supplies nothing, so records go to standard error. |
| AsTextHandler | Annotates a constructor returning a TextHandlerProvider, or a value of that type, as Params.TextHandler. |
| WithTextHandler | Supplies Params.TextHandler. Any function with the provider's signature converts to it. |
| WithMiddleware | Adds a Middleware value to the chain, at the order it declares. A nil Middleware supplies nothing. |
| ProvideMiddleware | Adds the middleware a constructor builds from the graph to the chain, at the order it declares. The constructor returns any type implementing Middleware. A constructor returning a nil Middleware leaves it out. |
| NewEventLogger | An fx event logger, for fx.WithLogger, that writes fx's events at Debug and its failures at Error. |
| Writer | An io.WriteCloser that logs each line written to it. Safe for concurrent use. |
| NewWriter | Returns a Writer that logs through a logger at a level, with a context, and with the source location of the NewWriter call. |
| Writer.Write | Logs each complete line and buffers the rest. Always reports every byte written and a nil error. |
| Writer.Close | Logs any buffered partial line. The writer stays usable. |
| WriterProvider | Returns a Writer that logs at a level, with a context and with optional attributes on every record, through the logger it was built with. Records carry the source location of the WriterProvider call. |
| WriterProviderParams | NewWriterProvider's inputs. |
| WriterProviderParams.Logger | The logger every Writer logs through. |
| NewWriterProvider | Returns a WriterProvider over a logger. Module provides it. |
| tint.New | A TextHandlerProvider rendering with lmittmann/tint, coloured only on a terminal with NO_COLOR unset or empty. For services. |
| tint.ProvideTextHandler | Supplies tint.New as Params.TextHandler. |
| charm.New | A TextHandlerProvider rendering with charm.land/log, with groups as dot-qualified keys and levels between the standard ones labelled as slog names them. For operator tools. Ignores ReplaceAttr. |
| charm.ProvideTextHandler | Supplies charm.New as Params.TextHandler. |

## Usage

### Standalone

Fill `Params` by hand. Every field is optional; the zero value logs JSON from Info up to standard error:

```go
import (
	"github.com/sikhlana/pkg/log"
	"github.com/sikhlana/pkg/log/tint"
)

logger, err := log.New(log.Params{
	Debug:       debug,
	Format:      log.FormatText,
	TextHandler: tint.New,
	Middleware: []log.Middleware{
		log.Ordered(500, log.MiddlewareFunc(requestID)),
	},
})
if err != nil {
	return fmt.Errorf("building logger: %w", err)
}
```

`Format` decodes from `json` or `text`, so a flag parses it and rejects anything else before the program starts:

```go
var format log.Format

cmd.Flags().TextVar(&format, "format", log.FormatText, "log format: json or text")
```

JSON records always carry their source location, for the log pipeline. Text records carry it only under `Debug`, so an operator's normal output stays short.

#### Text handlers

The text handler is pluggable, so its dependencies are built only by the programs that pick it:

- `log/tint` suits services: compact and coloured, but only when the destination is a terminal and `NO_COLOR` is unset or empty.
- `log/charm` suits operator tools, with charm's styles. It writes attributes inside groups as `http.status=200`, as slog's text handler does, rather than as a prefix on the message, as charm's own slog support does. A level between the standard ones is labelled as slog names it (`DEBUG-4`, `INFO+2`, `ERROR+4`) from Debug-8 to Error+8, and with the nearest standard label beyond that; charm's `FATAL` never appears.
- Without either, text goes through slog's `TextHandler`: plain `key=value`, no colour, no extra dependencies.

Importing `log` alone never builds tint or charm.

#### Attributes

Log with typed attributes (`slog.String`, `slog.Int`, `slog.Duration`, `slog.GroupAttrs`), never key-value pairs, and `slog.Any` only where no typed constructor fits. The repo's lint config enforces it.

Use the `*Context` methods (`InfoContext`, `LogAttrs`) wherever a context is in scope: middleware only sees the context the record was logged with.

Groups that share a key with a sibling, at any depth, merge into one at the position of the first, including groups attached with `With` and groups inside a group opened with `WithGroup`. Prefer Datadog's standard attribute names (`http.method`, `http.status_code`, `network.client.ip`): a module's `http` group and a middleware's `http.request_id` then write one `http` object, not two `"http"` keys that a JSON parser would collapse to the last. Two plain attributes with the same key stay two attributes.

#### Middleware

A middleware sees every record that passes the level, including records from loggers derived with `With` and `WithGroup` and records handed straight to the handler. Its `Handle` calls `next` to pass the record on, changed or not, and returns without calling it to drop the record. A function adapts through `MiddlewareFunc`:

```go
type requestIDKey struct{}

func requestID(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.GroupAttrs("http", slog.String("request_id", id)))
	}

	return next(ctx, r)
}
```

- **It sees the whole record.** Attributes a logger carries from `With`, and the groups it opened with `WithGroup`, are folded into the record before the chain runs, so a redactor sees them as it sees attributes passed to the call. In exchange, a record from a logger with `With` attributes or an open group is rebuilt every time, rather than having those attributes encoded once.
- **Attributes land at the top level.** The record a middleware sees is already rebuilt relative to the root, so `request_id` above sits beside the caller's groups, never inside a group the caller opened with `WithGroup`.
- **No handler plumbing.** The middleware never implements `WithAttrs` or `WithGroup`, so `logger.With(...)` can't skip it.
- **No import needed.** `Handle`'s signature uses only standard-library types, so a package can implement `Middleware` without importing `log`. It clashes with `slog.Handler`'s `Handle`, so a type can't be registered as the wrong one.
- **Lower orders run first,** nearest the caller, and each middleware sees what lower ones passed on. A middleware declares its order with `Order() int`; one that doesn't counts as 0. Leave gaps, and use bands: samplers and filters around 100, so dropped records cost nothing further; attribute injectors around 500; redactors around 900, so they see everything the others added. Declare an order wherever the place matters.
- **The app can override it.** `log.Ordered(n, m)` runs `m` at `n`, replacing any order it declares, for middleware you didn't write or a function adapted with `MiddlewareFunc`.
- **Equal orders run in an unspecified order,** which can change from one run to the next. Give distinct orders wherever it matters.
- **Nil means off.** A nil `Middleware`, a nil pointer, func, map, slice or channel behind one, or either wrapped by `Ordered`, is left out, so a constructor can return nil when its feature is off.
- **Clone before keeping.** A record shares its attribute storage with the caller's copy. A middleware that keeps a record, or passes it on more than once, clones it with `r.Clone()` first.

#### Subprocess output

`NewWriter` logs each line written to it, so a subprocess's output becomes records:

```go
stdout := log.NewWriter(ctx, slog.LevelInfo, logger)
stderr := log.NewWriter(ctx, slog.LevelWarn, logger)

cmd := exec.CommandContext(ctx, "terraform", "plan")
cmd.Stdout, cmd.Stderr = stdout, stderr

err := cmd.Run()

_ = stdout.Close()
_ = stderr.Close()
```

- **Source location:** every record carries the location of its `NewWriter` call, not of the goroutine that copies the output.
- **Context:** records go through the logger's handler with the writer's context, so middleware runs on them.
- **Lines:** a carriage return before the newline is stripped, empty lines are skipped, and `Close` logs a last line without a newline.
- **Long runs:** every run of 64 KiB without a newline is logged as a record of its own, cut after its last complete UTF-8 character, however the writes arrive. A stream without newlines can't grow memory without bound, and the writer never buffers more than one such run.
- **Errors:** below the logger's level, a writer discards what it's given. `Write` never fails, whatever the handler returns, so a failing log destination never breaks the subprocess.

### With fx

List `Module` and supply the wiring the program needs. Route fx's own events through the logger with `NewEventLogger` at the top level of the app; inside an `fx.Module` it would only see that module's events:

```go
app := fx.New(
	fx.WithLogger(log.NewEventLogger),
	log.Module,
	log.WithDebug(debug),
	log.WithQuiet(quiet),
	log.WithFormat(format),
	tint.ProvideTextHandler(),
	log.WithMiddleware(log.Ordered(500, log.MiddlewareFunc(requestID))),
	log.ProvideMiddleware(newSampler),
	// The rest of the app.
)
```

fx's provide, invoke and lifecycle events show only under debug; its failures show in every mode, quiet included.

`Module` also provides a `WriterProvider`, so a module that runs subprocesses takes it from the graph instead of the logger. Attributes given to it go on every record from that writer, and each record carries the location of the provider call:

```go
type Params struct {
	fx.In

	NewWriter log.WriterProvider
}

func run(ctx context.Context, p Params) error {
	stdout := p.NewWriter(ctx, slog.LevelInfo, slog.String("stream", "stdout"))
	stderr := p.NewWriter(ctx, slog.LevelWarn, slog.String("stream", "stderr"))

	cmd := exec.CommandContext(ctx, "terraform", "plan")
	cmd.Stdout, cmd.Stderr = stdout, stderr

	err := cmd.Run()

	_ = stdout.Close()
	_ = stderr.Close()

	return err
}
```

`WithMiddleware` registers a value. `ProvideMiddleware` registers a constructor that builds the middleware from the graph, returning any type that implements `Middleware`; both are ordered together. A middleware that belongs at a particular place declares it:

```go
type sampler struct {
	rate float64
}

func newSampler(cfg SamplerConfig) sampler {
	return sampler{rate: cfg.Rate}
}

func (s sampler) Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error {
	if r.Level < slog.LevelWarn && rand.Float64() >= s.rate {
		return nil
	}

	return next(ctx, r)
}

func (sampler) Order() int {
	return 100
}
```

A constructor whose middleware depends on a condition known only when it runs returns nil to leave it out. It has to return the `Middleware` interface for that, since fx keeps whatever it's given:

```go
log.ProvideMiddleware(func(cfg SamplerConfig) log.Middleware {
	if cfg.Rate >= 1 {
		return nil
	}

	return newSampler(cfg)
})
```

Each `WithX` helper supplies a value; its `AsX` twin annotates a constructor for `fx.Provide`, for a value the graph builds:

```go
fx.Provide(log.AsDebug(func(flags Flags) bool {
	return flags.Debug
}))
```

To log to a file, open it before the app and close it after `Stop` returns. A stop hook would close it too early: the logger is built first, so its hooks run last, and fx's final events and anything logged after `Stop` would go to a closed file and vanish:

```go
f, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
if err != nil {
	return fmt.Errorf("opening the log file: %w", err)
}

defer f.Close()

app := fx.New(
	fx.WithLogger(log.NewEventLogger),
	log.Module,
	log.WithDestination(f),
	// The rest of the app.
)
```
