# Logging

[log/README.md](../log/README.md) says how to build the logger, register middleware and pick a text handler. This page says why the module is shaped that way. The full discussion, including the alternatives rejected, is in the archived [add-log](../openspec/changes/archive/2026-10-17-add-log/design.md) change.

## Nothing to configure

The level, format and destination are developer wiring, not Config. They change per run, not per deployment: an operator passes `--debug`, `--quiet` or `--format` to one invocation rather than editing a file. The logger also has to exist before any config is loaded, to report what goes wrong while loading it. The app developer picks each flag's default, JSON for a service and text for an operator tool, and the kernel maps the flags onto the module's `WithX` helpers.

Quiet is a level, Warn and above, not a handler that discards everything. A quiet run still shows its warnings and errors, and fx's failures, which the event logger writes at Error.

## Middleware is a step a record passes through

A middleware's `Handle(ctx, r, next)` gets each enabled record and calls `next` to pass it on, changed or not, or returns without calling it to drop the record. It never implements `slog.Handler`. Two alternatives lost:

- **Handler middleware (`func(slog.Handler) slog.Handler`)** has to re-wrap itself in `WithAttrs` and `WithGroup`. One that forgets is skipped by every logger derived with `With`, without any error, and `With` is on every request path.
- **Attribute extractors (`func(ctx) []slog.Attr`)** can add attributes but can't drop or rewrite a record, so samplers and redactors would need a second mechanism.

`Middleware` is an interface rather than a function type so that a middleware can declare its order with an `Order()` method; `MiddlewareFunc` adapts a function. The method is named `Handle`, after `slog.Handler`'s, so the two clash and no type can be both, as httpserver's `ServeHTTP(w, r, next)` clashes with `http.Handler`'s. Its signature uses only standard-library types, so a module can implement it without importing `log`, and the app registers it. That keeps to the rule that modules don't import siblings just to integrate.

## The middleware declares its order

Middleware runs in the order it declares, overridable by the app with `Ordered`, and a nil middleware is left out; [middleware.md](middleware.md) says why, for log and httpserver alike. What's specific to log is the bands: samplers around 100, so a dropped record costs nothing further; attribute injectors around 500; redactors around 900, so they see what the others added. A module can't see another's numbers without importing it, so the bands, written in log's README, are what keep independently written middleware apart. `telemetry.LogMiddleware` declares 500 for that reason.

## Middleware sees the whole record

The core handler holds every attribute given to `With` and every group opened with `WithGroup`, and folds them into each record before the chain runs. Nothing reaches the base handler through `WithAttrs` or `WithGroup`. That buys three things:

- **Redactors see everything.** A secret attached with `With` passes through the chain like one passed to the call.
- **Added attributes land at the top level.** The chain sees the record relative to the root, so a request id sits beside the caller's groups, never inside a group the caller happened to open.
- **The caller's attributes keep their order,** and the chain is built once, because the base never changes.

The price is that a record from a logger with `With` attributes is rebuilt every time, and the base encodes those attributes per record instead of once. The first implementation forwarded plain attributes to the base until the first group to save that work. Code review found that redactors then couldn't see them, and that splitting them from held groups reordered the caller's attributes.

## Groups that share a key merge

slog never merges groups, and its JSON handler writes duplicate keys, which most parsers collapse to the last. Datadog's standard attributes are grouped (`http.method`, `http.request_id`, `network.client.ip`), so an access log's `http` group and a middleware's `http.request_id` would otherwise produce two `"http"` keys and lose one.

So before a record is written, groups that share a key with a sibling merge into the first, at any depth. Plain attributes that share a key stay as they are: picking one silently would hide a bug. The check allocates nothing when there's nothing to merge.

## Text handlers import log, never the reverse

`log/tint` and `log/charm` each export `New` and `ProvideTextHandler`, built on core's `WithTextHandler`. Core never imports either, so charm's terminal stack is built only by apps that import `log/charm`. Both stay in `log`'s go.mod, as the subpackage rule has it. A charm bump therefore means a `log` patch release even for tint users, accepted until the churn says otherwise. Without a text handler, core falls back to the standard library's, and the kernel picks tint by default.

Charm's own slog support doesn't fit slog's rules, so `log/charm` adapts it:

- **Groups are a prefix in charm.** Its `WithGroup` sets the message prefix, so the adapter tracks groups and writes dot-qualified keys, as slog's text handler does.
- **Charm only prints a level it has a style for.** The adapter gives it styles for the levels between the standard ones, labelled as slog names them. It sets them all before the logger is shared, because charm reads its styles pointer without a lock.
