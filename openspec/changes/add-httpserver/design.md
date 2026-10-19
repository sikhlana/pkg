## Context

See proposal.md for why the server is ported and what's left behind. The requirements are in specs/httpserver/spec.md. This document covers how they're met and what was rejected. The shape was worked out in an explore session against go-modules' `http/server` at `ff54db7`; each block was rethought rather than copied, and no comment or godoc carries over.

Facts that shape the approach, to confirm against the toolchain and the libraries' official docs in task 1.3:

- **`http.Server.ServeTLS(l, "", "")` uses `TLSConfig.GetCertificate` when both file names are empty**, serves on a clone of `TLSConfig`, and adds `h2` and `http/1.1` to the clone's `NextProtos` when HTTP/2 is enabled. tlsconfig's README says a copy keeps refreshing, so serving on the clone keeps rotation.
- **With `http.Server.Protocols` nil, net/http serves HTTP/1.1 and, over TLS, HTTP/2**, and doesn't serve cleartext HTTP/2.
- **`http.Server.Shutdown` closes the listeners, then waits for idle connections until its context ends**, returning the context's error then. Connections still active stay open until `Close`.
- **`slog.NewLogLogger(h, level)` returns a `*log.Logger` that writes each line as one record at that level**, which is what `http.Server.ErrorLog` takes.
- **dig doesn't guarantee the order of a value group's members**, so registration order can't decide anything.
- **go-playground's `hostname_port` tag** may or may not accept an empty host (`:8080`) and port 0. If it rejects either, the address check is a `Validate` step over `net.SplitHostPort` instead.

Confirmed in task 1.3 against Go 1.27.2's `go doc` and pkg.go.dev:

- `ServeTLS` uses `TLSConfig.GetCertificate` when it's set and the file names are empty, and documents that it clones `TLSConfig`. net/http's package doc says the server enables HTTP/2 automatically over HTTPS, and `Protocols` nil means "usually HTTP/1 and HTTP/2". The tests check ALPN from the client's connection state.
- `Shutdown` returns the context's error when it expires first, and makes `Serve` return `http.ErrServerClosed` at once, so `Serve` has to wait for the draining goroutine rather than return on `ErrServerClosed`.
- `slog.NewLogLogger` dispatches each `Output` call as one record at the given level.
- **`hostname_port` differs.** It accepts `:8080` and `0.0.0.0:80` but rejects port 0 (`127.0.0.1:0`, `:0`) and bracketed IPv6 (`[::1]:8080`); pkg.go.dev doesn't document either case, so this was checked through `configvalidator.Validate`. After a first pass with a single `address` checked by `net.SplitHostPort`, the user chose to go back to separate `host` and `port` fields so the check is go-playground tags alone (decision 8).

## Goals / Non-Goals

**Goals:**

- Several servers in one app, declared once, run per command.
- Nothing about a server (constructor, middleware, TLS files, socket) is touched in a command that doesn't use it.
- A server built by hand behaves like one built under fx: same Listen, Serve and Close, same chain ordering.

**Non-Goals:**

- Any concrete middleware: recovery, tracing, request ids, access logs, CORS. Consumers or later modules provide them through the group.
- Client IP resolution and trusted proxies.
- Cleartext HTTP/2 (h2c), Unix sockets, socket activation, and listeners supplied from outside.
- Routing. The handler is whatever `http.Handler` the app builds.

## Decisions

### 1. Layout and identity

```
http/httpserver/            module github.com/sikhlana/pkg/http/httpserver
├── config.go               Config, Default* timeouts, resolvers, ErrNegativeTimeout
├── httpserver.go           Server, Provider, NewProvider, ProviderParams
├── middleware.go           Middleware, OrderedMiddleware, MiddlewareFunc, Ordered, Chain
├── module.go               Module, ProvideServer, ProvideMiddleware, NameTag, MiddlewareGroup
├── runner.go               ProvideRunner
└── *_test.go
```

Package `httpserver`, fx.Module name `httpserver`. Tags: `name:"httpserver:server:<name>"` for a server, `group:"httpserver:middleware:<name>"` for its middleware. The constructor's result, before its middleware is applied, is `name:"httpserver:server::built"`, an action segment set off by a double colon. Two unexported groups carry names for the check in decision 7.

### 2. Exposure: a Provider, with ProvideServer tagging by name

`Provider` is `func(Config, http.Handler) (*Server, error)`, built by `NewProvider(ProviderParams)` over the injected logger. `ProviderParams.Logger` is `optional:"true"` (decided during apply): with no logger in the graph, or a nil one filled in by hand, records are discarded, as kernelrun does. The Provider also validates the Config it's handed, per the constitution's rule for Provider closures, so the error can be a configvalidator violation too. It's the constitution's Provider shape: the same kind of server, built per Config. The error is the TLS build's, and `ErrNilHandler` for a nil handler, since falling back to `http.DefaultServeMux` would serve a global by accident.

`ProvideServer(name, ctor)` takes a constructor of the app's own, which injects the Provider, its Config and its handler and returns `(*Server, error)`. The helper provides the result privately, collects `MiddlewareGroup(name)`, builds the chain around the server's handler, names the server for logging, and provides it under `NameTag(name)`. So `*Server[name]` always means the server with its middleware, and the name is written once.

```go
httpserver.ProvideServer("api", func(p APIServerParams) (*httpserver.Server, error) {
	return p.Provider(p.Config, p.Handler)
})
```

The chain is applied before anything can see the server, so setting the handler and name on it inside the helper is a write to a value nobody else holds yet.

**Alternatives:** a singleton built from the graph's Config plus a handler, with a default runner (one server per app, and a second one wired differently from the first); go-modules' `ProvideServer`, which reflected over the constructor's return types to find a Config and a handler in either order and built the server itself (reflection, panics on misuse, three tag helpers); the Provider taking the name (written twice, and free to drift from the tag).

### 3. The runner owns the lifecycle

```
 Provider(cfg, h)       → inert *Server: config, chain, tls.Config, logger. No socket, no hooks.
 ProvideRunner(name)    → OnStart  s.Listen(ctx)    bind; a failure is a startup failure
                          runner   s.Serve(ctx)     "httpserver:<name>"
                          OnStop   s.Close()        release a listener Serve never took over
```

`ProvideRunner(name)` registers through `kernelrun.ProvideRunner("httpserver:"+name, ...)`. Its constructor takes `fx.Lifecycle` and the server under `NameTag(name)` as optional; a missing server returns `ErrUnknownServer` naming it, so the check needs no ordering against kernelrun's own. The prefix keeps a server called `migrate` from colliding with another module's runner of that name.

Found while implementing: fx can't annotate a constructor twice, and `kernelrun.ProvideRunner` annotates its own, so `ProvideRunner` can't hand it a constructor with `fx.ParamTags`. It wraps both in an `fx.Module("httpserver")`: a private adapter resolves the optional server under `NameTag(name)` into an unexported `runnerServer`, and the constructor passed to `kernelrun.ProvideRunner` takes that.

**Alternatives:** the Provider appending the hooks when it builds a server (anything that injects a server, to read its address say, would bind a socket nobody serves, leaving clients hanging in the backlog); binding inside `Serve`, as go-modules did (against the constitution: a port conflict surfaces after startup, and a finished start doesn't mean ready).

### 4. Server's states

| State | Listen | Serve | Close |
|---|---|---|---|
| built | binds → listening | ErrNotListening | no-op |
| listening | ErrAlreadyListening | serves → serving | closes the listener → closed |
| serving | ErrAlreadyListening | ErrAlreadyServing | no-op: Serve owns the listener |
| closed | ErrAlreadyListening | ErrNotListening | no-op |

Once `Serve` returns, the server moves to closed, so a second `Serve` fails with `ErrNotListening` (found while implementing; the table's serving row covers only a call made while the first is running). A listener failure while serving also closes every remaining connection, since nothing else will. A mutex guards the state; Server takes pointer receivers. `Addr()` returns the bound address, or nil before Listen. A Server isn't reusable after Close: an app builds a new one.

### 5. Serve and shutdown

`Serve` runs `http.Server.Serve`, or `ServeTLS(l, "", "")` when the server has a `tls.Config`. A goroutine waits for the context, logs that it's draining, and calls `Shutdown` with `context.WithoutCancel(ctx)` bounded by `shutdown_timeout`. kernelrun waits for every runner before its own stop timeout applies, so the bound has to live here. When `Shutdown` runs out of time, `Serve` calls `Close` to drop the remaining connections and returns `fmt.Errorf("draining connections: %w", err)`, which matches `context.DeadlineExceeded` and which kernelrun reports as a crash: requests were cut, and the operator should hear about it. A clean drain returns nil. Any serve error other than `http.ErrServerClosed` is returned wrapped.

`Protocols` stays nil, so net/http serves HTTP/1.1, plus HTTP/2 over TLS, and no h2c. go-modules set the protocols explicitly and wired ALPN into a hand-built TLS listener; `ServeTLS` does both.

### 6. Middleware

```go
type Middleware interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler)
}

type OrderedMiddleware interface {
	Middleware
	Order() int
}
```

`Chain(h, mws...)` sorts by order, ascending, then builds from the inside out, once: each position's `next` is a fixed `http.HandlerFunc` calling the next middleware's `ServeHTTP` with the handler after it. Each position is a small pointer type holding the middleware and its `next` rather than a closure, so `next` has an identity the tests can compare (found while implementing). A middleware that isn't an `OrderedMiddleware` counts as 0. `Ordered(order, m)` returns an `OrderedMiddleware` whose order replaces `m`'s own. `MiddlewareFunc` is a function type implementing `Middleware`. `ProvideMiddleware(name, ctor)` annotates `ctor` with `fx.As(new(Middleware))` and `fx.ResultTags(MiddlewareGroup(name))`, so a constructor can return its concrete type; `fx.As` checks at build that it implements `Middleware`.

Ties don't fail. dig shuffles the group, so equal orders run in an order that can change between starts; the sort is stable, which only matters for `Chain` called by hand. Most middleware doesn't care where it sits, and the ones that do declare an order or are given one with `Ordered`.

A type can't be both an `http.Handler` and a `Middleware`, since the two `ServeHTTP` methods clash. That's accepted: it stops a handler being registered as middleware.

Ecosystem middleware written as `func(http.Handler) http.Handler` adapts through `MiddlewareFunc`. otelhttp's `NewMiddleware` does its setup once and returns a wrapper that only allocates a closure, so wrapping per request costs one small allocation. That's the common convention, so there's no special adapter.

**Alternatives:** `Wrap(next http.Handler) http.Handler` plus `Order()` (rejected by the user in favour of the net/http-like signature, with `next` owned by httpserver); failing startup on equal orders (rejected: most middleware is order-agnostic, and a tie-break by type name would hide the shuffle without fixing it); go-modules' two fixed slots for a request id and an access log (one slot per concern doesn't scale, and the slots tied httpserver to those modules).

### 7. Checking names without building anything

`ProvideServer(name)` also supplies `name` into an unexported group of server names; `ProvideMiddleware(name)` supplies it into an unexported group of middleware targets. `Module` invokes a check that takes only those two string groups and fails with `fmt.Errorf("%w: %q", ErrUnknownServer, name)` for a target that isn't a server. Supplied strings cost nothing to build, so the check runs in every command without constructing a server or a middleware. The runner side is covered in decision 3.

So Module has an invoke, but one that never reaches a server. Duplicate `ProvideServer` names fail through fx's own duplicate-provider error.

### 8. Config

```go
type Config struct {
	Host              string                 `mapstructure:"host" validate:"omitempty,hostname_rfc1123|ip"`
	Port              int                    `mapstructure:"port" validate:"gte=0,lte=65535"`
	ReadTimeout       time.Duration          `mapstructure:"read_timeout"`
	ReadHeaderTimeout time.Duration          `mapstructure:"read_header_timeout"`
	WriteTimeout      time.Duration          `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration          `mapstructure:"idle_timeout"`
	ShutdownTimeout   time.Duration          `mapstructure:"shutdown_timeout"`
	TLS               tlsconfig.ServerConfig `mapstructure:"tls"`
}
```

`host` and `port` stay separate, as in go-modules, so go-playground's tags check them and an operator can override the port alone: `host` takes `omitempty,hostname_rfc1123|ip` (empty listens on every interface; IPv6 is written bare, `::1`), `port` takes `gte=0,lte=65535`. The `port` tag isn't used because it rejects 0, which picks a free port, and tests bind `127.0.0.1:0`. The cost, accepted by the user: a config that forgets the port gets a random one rather than failing. An unexported `address()` joins them with `net.JoinHostPort`; there's no exported `Addr()` on the Config. (The proposal first had a single `address`; task 1.3 found `hostname_port` rejects port 0 and IPv6, and the user preferred tags to a hand-written `net.SplitHostPort` check.) `shutdown_timeout` replaces `stop_timeout`, which would be confused with kernelrun's stop timeout for OnStop hooks. Zero means the `Default*` constant through an unexported resolver for each timeout. `Validate` joins `ErrNegativeTimeout` for each negative timeout, each as a `configvalidator.Error` naming its key, with `configvalidator.ValidateField(&c, &c.TLS)`, since a Config with its own `Validate` owns its subtree.

The `tls.Config` is built in the Provider, so a bad key pair fails the build, which happens in the app's construction phase, before any start hook.

### 9. Logging

The Provider's logger is the injected one; inside `ProvideServer`, the constructor's Provider is decorated with one over `logger.With(slog.String("server", name))` (see Fixes from code review). Records: Info `http server listening` with `address` on Listen, Info `http server draining` when the context ends, Debug `http server stopped` after a clean drain. `http.Server.ErrorLog` is `slog.NewLogLogger(handler, slog.LevelDebug)` over the named logger, so handshake failures from scanners don't fill the logs. The trade-off: net/http's report of a panicking handler also lands at Debug. A recovery middleware is the answer, and the README says so.

### 10. Constitution amendments

- **Named instances.** The run-loop rule's `ProvideRunner()` with `DefaultRunnerName` stays for singletons. A module whose loops are named instances takes the name, `ProvideRunner(name)`, and registers as `<module>:<name>`.
- **Per-name groups.** A group keyed by an instance name can't be a constant, so it's exposed as a function returning its tag, `MiddlewareGroup(name)`, beside its typed `ProvideX` helper and its collector, as name tags already are with `NameTag(name)`.
- **Action segments.** Added after the second review: a segment naming an action done to the value (built, wired, supplied) is separated by a double colon, `name:"<module>:<purpose>::<action>"`. Applied to kernelrun's and log's built tags and telemetry/trace's wired and supplied tags, which each take a patch bump.
- **Instance names.** Non-empty, letters, digits, `-`, `_` and `.`, checked by every helper taking one, failing startup through `fx.Error` with `ErrInvalidName`. Names can't hold `:`, so they can't produce an action segment.
- **Positional parameters.** The single-Params rule's exception: a constructor whose tags depend on an instance name given at wiring time takes positional parameters with `fx.ParamTags`.

### Rollup into docs/http.md at archive

Decisions 3 (why the runner owns the lifecycle, and why the Provider must not append hooks), 6 (why order comes from `Order()`, why ties don't fail, and the three-argument signature), and 7 (checking names without building).

## Risks / Trade-offs

- [Equal orders run in a different order between starts] → Documented in the README and spec; an order-sensitive middleware declares an order or is given one with `Ordered`.
- [A panicking handler is only reported at Debug without a recovery middleware] → README recommends one at a low order.
- [A shutdown that runs out of time is reported as a crash, so the process exits non-zero after a SIGTERM] → Intended: dropped requests are worth hearing about. The README tells operators to keep `shutdown_timeout` below the orchestrator's grace period, so the drain finishes before a SIGKILL.
- [`ProvideServer`'s constructor can still build a server through the Provider with the wrong name's Config] → The tag is on the result, not the Config; the README's example gives each module its own Config type embedding `httpserver.Config`, so each constructor asks for its own section without tags.
- [A config that forgets `port` listens on a random port] → Accepted with separate `host` and `port` (decision 8); the README says to set the port explicitly.

## Open after apply

- `config/configloader` is a test-only requirement, for the load-through-configloader scenarios, so viper appears in the module graph (not the build) of consumers.
- govulncheck reports GO-2026-5932 (golang.org/x/crypto/openpgp) at module level only, through configvalidator's indirect requirement; no code calls it.
- kernelrun.ProvideRunner annotates its constructor itself, and fx can't annotate a constructor twice, so a named-instance runner can't put its name tag on kernelrun's constructor. httpserver works around it with a private `runnerServer` adapter; every later named-instance module would copy it. A follow-up change could let kernelrun.ProvideRunner take `fx.ParamTags`.

## Fixes from code review

A review of the branch against `origin/main` found these; each fix has a test that fails without it.

- **A TLS listener leaked when `ServeTLS` failed before serving.** `ServeTLS` can return before handing the listener to `http.Server.Serve`, whose deferred close is what releases it, and the state was already closed, so the OnStop `Close` did nothing. Serve's failure path now closes the listener itself. Practically unreachable through tlsconfig, but cheap.
- **tlsconfig's records and net/http's error log didn't name the server.** `ProvideServer` named the server's logger after the Provider had built the TLS configuration with the unnamed one. Now `ProvideServer`'s module decorates the Provider its constructor receives with one over the named logger, so every record from the build on carries `server`. The decorated Provider replaces the app's for that constructor, so a decoration of the app's Provider doesn't reach it; nothing in the repo decorates it, and the godoc says so. The decorator takes the app's Provider, unused, so `Module` stays required. (Replaces the open item about tlsconfig's records.)
- **A constructor returning `(nil, nil)` panicked.** `ProvideServer` now fails startup with `ErrNilServer`, naming the server.
- **`MiddlewareGroup`'s godoc suggested a route that skipped the name check.** Registering into the group directly never supplies the target name, so a typo went silently unserved. The godoc and README now say to add with `ProvideMiddleware` only.
- **A drain timeout's verdict depended on how the run ended.** kernelrun counts an error matching the run context's error as clean, and the drain's error wrapped `context.DeadlineExceeded`, so a run ended by deadline counted cut requests as clean. Serve now returns `ErrShutdownTimeout`, which doesn't match it. Spec scenario updated.
- **Handlers didn't see the serve context's values.** The `http.Server` now has a `BaseContext` of `context.WithoutCancel(ctx)`, matching what the drain already used: values pass through, cancellation doesn't, so ending the context drains requests rather than cancelling them.
- **A second `Serve` while serving returned `ErrAlreadyListening`,** which reads like a bind problem. It returns the new `ErrAlreadyServing`.
- **Timeouts can't be turned off.** Kept: zero means the default, so every server bounds slow clients. The README points streaming and upload handlers at `http.ResponseController`'s per-request deadlines.
- **The TLS section is validated twice per build,** by the Provider's `configvalidator.Validate` and again inside `TLSConfig`. Kept: the constitution has Provider closures validate the Config they're handed, `TLSConfig` validates by contract, and the cost is negligible.

The README's With fx example now shows a module that owns its server, with a Config embedding `httpserver.Config` under `mapstructure:",squash"`, and explains that the embed promotes `Validate`. Tests load such a config through configloader, with and without a `Validate` of the module's own.

## Fixes from the second code review

- **A server named `built` made a dependency cycle.** The constructor's private result tag was `name:"httpserver:server:built"`, exactly `NameTag("built")`. It's now `httpserver:server::built`, and the constitution's action-segment rule keeps every such tag apart from names built from instance names.
- **A nil middleware failed every request.** First fixed by failing startup with `ErrNilMiddleware` and panicking in `Chain`; then reversed in discussion, because fx keeps nil values in a group and a constructor is the only place a condition from the loaded config can decide whether a middleware applies. `Chain`, which `ProvideServer` uses, now leaves out a nil `Middleware`, a nil pointer, func, map, slice or channel behind one (checked once per middleware with `reflect`, at build), and either wrapped by `Ordered`, so `return nil` is the documented way to leave a middleware out. A nil pointer whose methods would have worked is left out too: returning one almost always means none. Rejected alternatives: a pass-through middleware (a hop per request, intent hidden) and a `flatten` group returning `[]Middleware` (a second helper for the same job).
- **Default timeouts cap TLS handshakes at 1s.** net/http bounds a handshake by the smallest positive of read_header_timeout, read_timeout and write_timeout (confirmed in the installed toolchain's `tlsHandshakeTimeout`). First kept the 1s default and documented it; then, after a retrospective, raised `DefaultReadHeaderTimeout` to 5s, matching `DefaultReadTimeout`, because a handshake that runs out of time surfaces only as a Debug record. 5s still bounds slowloris-style header dribbling, since a whole request is bounded by the same 5s anyway. The README and godoc explain how the handshake's bound is derived.
- **A listener failure racing the context could wait out a full drain.** Serve now runs the drain through `context.AfterFunc` and, on a serve failure, closes the listener and server before waiting for a drain that started meanwhile, which ends it at once, and joins whatever that drain returned into the serve error. Not covered by a test: the race can't be forced from outside.
- **Names weren't validated.** `ErrInvalidName` for empty names and characters outside letters, digits, `-`, `_` and `.`, in all three helpers.
- **The runner's constructor took positional parameters** without needing to; it takes `runnerParams`. The two constructors whose tags depend on the name stay positional, under the constitution's new exception.
- **kernelrun's README example didn't compile.** Rewritten as a self-contained singleton runner with a `RunnerParams` struct, pointing at httpserver for named instances.
- **ProvideServer wraps the constructor's server in place.** Kept; the godoc says the constructor returns a server of its own.

## Migration Plan

One pull request. `tls/tlsconfig`, `kernelrun` and `config/configvalidator` are already released, so `http/httpserver` requires their published versions and merging publishes `http/httpserver/v0.1.0`, plus patch releases of `kernel` and `kernelrun` for their README examples. No consumer exists yet.
