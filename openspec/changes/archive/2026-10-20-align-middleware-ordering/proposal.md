## Why

`log` and `http/httpserver` both collect middleware from an fx value group that dig shuffles, and both docs reason from that fact to opposite conclusions: `log` takes each middleware's order at registration and argues an `Order()` method means picking numbers blind, while `httpserver` puts the order on the middleware with `Ordered(n, m)` as the app's override. `log`'s model already leaks in practice, since `telemetry` ships `LogMiddlewareOrder` as a suggested number, which is the author default `httpserver` models directly. Two ordered groups with two rules invite a third with a third, so this change makes `httpserver`'s model the rule and moves `log` onto it.

## What Changes

- **BREAKING** `log.Middleware` becomes an interface with one method, `Handle(ctx context.Context, r slog.Record, next func(context.Context, slog.Record) error) error`. Its signature still uses only standard-library types, so a package implements it without importing `log`, and a type can't be both a `log.Middleware` and an `slog.Handler`, since the two `Handle` methods clash.
- **BREAKING** `log.OrderedMiddleware` becomes an interface adding `Order() int`, replacing the struct. A middleware that isn't one counts as order 0. `log.Ordered(order, m)` returns `m` at `order`, replacing any order it declares, so the app can place middleware it didn't write. `log.MiddlewareFunc` adapts a function with the old signature.
- **BREAKING** `log.WithMiddleware(m)` and `log.ProvideMiddleware(constructor)` lose their order parameter. `ProvideMiddleware`'s constructor returns any type implementing `Middleware`, and `Params.Middleware` becomes `[]Middleware`.
- **BREAKING** A nil `log.Middleware`, a nil pointer, func, map, slice or channel behind one, or either wrapped by `Ordered`, is left out of the chain, as `httpserver` already does, so a constructor can return nil for a condition known only when it runs. It replaces the rule that an `OrderedMiddleware` with a nil field is skipped.
- **BREAKING** `telemetry.LogMiddleware` becomes a type implementing `log.Middleware` and declaring order 500, the band `log`'s README gives attribute injectors, so samplers at about 100 run before it. `telemetry.LogMiddlewareOrder` is removed.
- `log` doesn't export a `Chain`: `New(Params{Middleware: ...})` is its standalone path.
- `httpserver` keeps its API. It gains no `WithMiddleware`: its middleware usually depends on config known only when the server is built, so a constructor is the form that fits.
- Constitution: a group whose members run in order declares that order on the member (an optional `Order() int`, default 0, with `Ordered(n, x)` to override), keeps the given order between equal ones within a slice, leaves the order across the group unspecified, and leaves nil members out.
- A new `docs/middleware.md` owns the shared reasoning (dig's shuffle, order on the value, the override, ties, nil members), and `docs/log.md` and `docs/http.md` point at it instead of each telling half of it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `log`: middleware runs in the order it declares, overridable at registration through `Ordered`, rather than the order given at registration; a nil middleware is left out.
- `telemetry`: the log middleware declares its place in `log`'s chain.

## Impact

- **`log`:** exported API changes (`Middleware`, `OrderedMiddleware`, `Ordered`, `MiddlewareFunc`, `WithMiddleware`, `ProvideMiddleware`, `Params.Middleware`), minor bump with `feat!` while below v1. Internally, `ProvideMiddleware`'s private adapter and its `log:middleware::built` tag go, since the group now holds `Middleware` itself, and `isNil` widens from `io.Writer` to any value.
- **`telemetry`:** exported API changes (`LogMiddleware`'s kind and order, `LogMiddlewareOrder` removed), minor bump with `feat!`.
- **`http/httpserver`:** no code change; its README and `docs/http.md` point at `docs/middleware.md`.
- **`kernel`:** none; it registers no log middleware.
- **No new dependencies.** `telemetry` still doesn't import `log`, so the two release in one tier.
- **Docs and repo files:** `openspec/config.yaml` (the ordered-group rule), `docs/middleware.md` (new), `docs/log.md`, `docs/http.md`, and the `log` and `telemetry` READMEs.
