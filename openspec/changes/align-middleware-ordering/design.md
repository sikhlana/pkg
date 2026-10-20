## Context

See proposal.md for why, and specs/log/spec.md and specs/telemetry/spec.md for the requirements. The shape was settled in conversation: a generic middleware module was considered first and rejected (below), and replacing log's middleware with `veqryn/slog-context` was considered and rejected too.

Today, log's middleware is a function type and its order lives beside it:

```go
type Middleware func(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error
type OrderedMiddleware struct{ Order int; Middleware Middleware }

log.WithMiddleware(telemetry.LogMiddlewareOrder, telemetry.LogMiddleware)
```

`ProvideMiddleware(order, constructor)` provides the constructor privately under `log:middleware::built`, then pairs it with its order in an adapter. `New` stable-sorts `Params.Middleware` by `Order`, skips entries with a nil `Middleware` field, and `newHandler` folds the rest into a chain of closures built once.

httpserver's model, which log moves onto, is described in docs/http.md § "Middleware order comes from the middleware".

## Goals / Non-Goals

**Goals:**

- log's and httpserver's middleware read the same way: an interface, an optional `Order() int`, `Ordered(n, m)` to override, a `MiddlewareFunc` adapter, nil members left out.
- A module exporting log middleware still doesn't import `log`.
- The chain is still built once in `New`, and a record's cost through it doesn't change beyond one interface call per link.

**Non-Goals:**

- Sharing code between log and httpserver. The two shapes can't share a type, and the shared part is a stable sort and a fold.
- Changing anything in httpserver's code.
- Deterministic order for equal orders. Both modules leave it unspecified, as now.

## Decisions

### 1. `Middleware` is an interface whose method is `Handle`

```go
type Middleware interface {
	Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error
}
```

A function type can't carry an `Order()` method, so the author's default order needs an interface. `Handle` mirrors httpserver's `ServeHTTP(w, r, next)`: it reads like the standard interface it sits next to, and it clashes with `slog.Handler.Handle(ctx, r)`, so no type is both a handler and a middleware, as no type is both an `http.Handler` and an httpserver middleware. The signature still uses only `context`, `log/slog` and a func, so `telemetry` implements it without importing `log`, which the constitution's "don't import a sibling to integrate" rule needs.

Alternatives: keep the function type and add an `Order` to a struct (today's model, which is what this change replaces); a method named `Process` or `Log`, which loses the clash, so one type could be both a handler and a middleware and be registered as the wrong one.

### 2. `OrderedMiddleware`, `Ordered` and `MiddlewareFunc` copy httpserver's

`OrderedMiddleware` embeds `Middleware` and adds `Order() int`. `Ordered(order, m)` returns an unexported wrapper that embeds `m` and reports `order`; given a wrapper, it unwraps first, so `Ordered(1, Ordered(2, m))` is `m` at 1, not a wrapper of a wrapper. `MiddlewareFunc` is the old function signature with a `Handle` method; a bare `MiddlewareFunc` has order 0, so existing functions migrate as `log.Ordered(500, log.MiddlewareFunc(f))`.

The godoc on each points at the same idea as httpserver's, in log's words: lower orders run first, nearest the caller.

### 3. Nil members are left out with the same reflection check

`New` drops a nil `Middleware`, a nil pointer, func, map, slice or channel behind one, and either wrapped by `Ordered`, before sorting. log's existing `isNil(io.Writer)` widens to `isNil(any)` and serves both the destination and middleware. Its `nilableKinds` keeps `reflect.Interface` out: `reflect.ValueOf` never returns an interface kind for a value held in an interface (the code-smell backlog flagged it as unreachable), so dropping it here is a tidy-up in a function the change already touches.

httpserver keeps its own copy. Sharing ten lines would need a module both depend on, which adds a release tier for nothing.

### 4. Ordering happens in `New`, for both paths

`New` filters, then stable-sorts by `orderOf(m)` (the declared order, or 0), then hands the result to `newHandler`. The fx path and `New(Params{Middleware: ...})` therefore order the same way, which the spec's "Outside fx" scenario needs. `chain` stays a fold over the sorted slice; each link calls `m.Handle(ctx, r, inner)`.

### 5. `ProvideMiddleware` annotates the constructor directly; `WithMiddleware` guards a nil interface

```go
func ProvideMiddleware(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.As(new(Middleware)), fx.ResultTags(MiddlewareGroup)))
}
```

The group now holds `Middleware` itself, so the private result, the `log:middleware::built` tag and the pairing adapter all go. The fx.Module wrapper that scoped the private provide goes with them. `ProvideMiddleware` still doesn't forward `fx.Annotation`s: the archived forward-fx-annotations design limits that to helpers whose callers need tags built at wiring time, and log's group is a constant.

`WithMiddleware(m)` supplies `m` with the same annotations. A nil interface has no type for `fx.Supply` to infer, so `WithMiddleware(nil)` returns `fx.Options()`, as `WithDestination(nil)` does. A typed nil (a nil `*T`) is supplied and then left out by `New`.

### 6. `telemetry.LogMiddleware` becomes an empty struct type at order 500

```go
type LogMiddleware struct{}

func (LogMiddleware) Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error
func (LogMiddleware) Order() int { return 500 }

log.WithMiddleware(telemetry.LogMiddleware{})
```

Its zero value is ready to use, and the type names the thing the README already calls it. 500 is the attribute-injector band in log's README; 100, the old suggestion, sits in the sampler band, where a sampler registered at, say, 150 would drop records the trace ids had already been added to.

Alternative: an exported variable of an unexported type (`telemetry.LogMiddleware` without braces). It reads slightly shorter at the call site but hides the type from godoc and can be reassigned.

### 7. telemetry's tests don't import log

The telemetry spec's "Its place in the chain" scenario spans two modules. Importing `log` in telemetry's tests would add it to telemetry's go.mod and make telemetry release after log, which this change otherwise avoids. Instead:

- log's tests cover the ordering rule (declared order, override, zero default, nil).
- telemetry's tests assert, through a local `interface{ Order() int }`, that `LogMiddleware` declares 500, and keep exercising `Handle` with a hand-written `next`.
- No test in the repo checks that `LogMiddleware` satisfies `log.Middleware`: neither module may import the other, and no module imports both. That's the one gap. A signature drift would surface only in an app that wires the two together, as a compile error at `log.WithMiddleware`.

Alternative: accept the test-only dependency. Rejected for the release coupling; revisit if a signature drift ever slips through.

### 8. The constitution rule and docs/middleware.md

The constitution gains one rule, beside the group-tag rules: a group whose members run in order declares that order on the member, with an optional `Order() int` defaulting to 0 and `Ordered(n, x)` to override; equal orders keep their given order within a slice and are unspecified across the group; nil members, including typed nils and wrapped ones, are left out. It doesn't mandate the `MiddlewareFunc` adapter, which only makes sense where a function shape exists.

`docs/middleware.md` takes the reasoning now split across docs/log.md § "The app orders the middleware" and docs/http.md § "Middleware order comes from the middleware" and "Leaving middleware out": dig shuffles groups, so order can't come from registration; the author knows the sensible default and the app knows the composition, so the order lives on the value with an override; ties don't fail startup; nil means off. docs/log.md and docs/http.md keep what's specific to them (log's whole-record view and group merging; httpserver's `next` built once and the `MiddlewareFunc` cost) and link to it. **Rollup at archive:** the rejected generic module and the rejected slogctx swap belong in docs/middleware.md as "alternatives considered", with a pointer to this change.

## Risks / Trade-offs

- [Every log middleware caller breaks] → Only `telemetry` and the READMEs use it in this repo, and both modules are below v1. The migration is mechanical (below).
- [An undeclared middleware runs at 0, before samplers at 100] → Same rule as httpserver. log's README tells authors to declare an order and keeps the bands; 0 means "don't care where".
- [`LogMiddleware`'s method set drifts from `log.Middleware` with no test to catch it] → Decision 7; caught wherever an app wires both, and both godocs name the signature.
- [Reflection on every middleware] → Once, in `New`, not per record.
- [docs/middleware.md is a third doc to keep in step] → It owns the shared part; the module docs stop repeating it, so the total shrinks.

## Migration Plan

One tier, two releases: `log` v0.2.0 and `telemetry` v0.2.0, both `feat!`, in either order, since neither imports the other. An app migrates:

```go
// before
log.WithMiddleware(500, requestID)
log.WithMiddleware(telemetry.LogMiddlewareOrder, telemetry.LogMiddleware)
log.ProvideMiddleware(900, newRedactor)

// after
log.WithMiddleware(log.Ordered(500, log.MiddlewareFunc(requestID)))
log.WithMiddleware(telemetry.LogMiddleware{})
log.ProvideMiddleware(func(...) log.Middleware { return log.Ordered(900, newRedactor(...)) })
```

A constructor whose middleware declares its own order needs no wrapping. Rollback is pinning the previous versions.
