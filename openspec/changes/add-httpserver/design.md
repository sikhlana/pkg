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

Package `httpserver`, fx.Module name `httpserver`. Tags: `name:"httpserver:server:<name>"` for a server, `group:"httpserver:middleware:<name>"` for its middleware. Two unexported groups carry names for the check in decision 7.

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
| serving | ErrAlreadyListening | ErrAlreadyListening | no-op: Serve owns the listener |
| closed | ErrAlreadyListening | ErrNotListening | no-op |

A mutex guards the state; Server takes pointer receivers. `Addr()` returns the bound address, or nil before Listen. A Server isn't reusable after Close: an app builds a new one.

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

`Chain(h, mws...)` sorts by order, ascending, then builds from the inside out, once: each position's `next` is a fixed `http.HandlerFunc` calling the next middleware's `ServeHTTP` with the handler after it. A middleware that isn't an `OrderedMiddleware` counts as 0. `Ordered(order, m)` returns an `OrderedMiddleware` whose order replaces `m`'s own. `MiddlewareFunc` is a function type implementing `Middleware`. `ProvideMiddleware(name, ctor)` annotates `ctor` with `fx.As(new(Middleware))` and `fx.ResultTags(MiddlewareGroup(name))`, so a constructor can return its concrete type; `fx.As` checks at build that it implements `Middleware`.

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

The Provider's logger is the injected one; `ProvideServer` adds `slog.String("server", name)`. Records: Info `http server listening` with `address` on Listen, Info `http server draining` when the context ends, Debug `http server stopped` after a clean drain. `http.Server.ErrorLog` is `slog.NewLogLogger(handler, slog.LevelDebug)` over the named logger, so handshake failures from scanners don't fill the logs. The trade-off: net/http's report of a panicking handler also lands at Debug. A recovery middleware is the answer, and the README says so.

### 10. Constitution amendments

- **Named instances.** The run-loop rule's `ProvideRunner()` with `DefaultRunnerName` stays for singletons. A module whose loops are named instances takes the name, `ProvideRunner(name)`, and registers as `<module>:<name>`.
- **Per-name groups.** A group keyed by an instance name can't be a constant, so it's exposed as a function returning its tag, `MiddlewareGroup(name)`, beside its typed `ProvideX` helper and its collector, as name tags already are with `NameTag(name)`.

### Rollup into docs/http.md at archive

Decisions 3 (why the runner owns the lifecycle, and why the Provider must not append hooks), 6 (why order comes from `Order()`, why ties don't fail, and the three-argument signature), and 7 (checking names without building).

## Risks / Trade-offs

- [Equal orders run in a different order between starts] → Documented in the README and spec; an order-sensitive middleware declares an order or is given one with `Ordered`.
- [A panicking handler is only reported at Debug without a recovery middleware] → README recommends one at a low order.
- [A shutdown that runs out of time is reported as a crash, so the process exits non-zero after a SIGTERM] → Intended: dropped requests are worth hearing about. The README tells operators to keep `shutdown_timeout` below the orchestrator's grace period, so the drain finishes before a SIGKILL.
- [`ProvideServer`'s constructor can still build a server through the Provider with the wrong name's Config] → The tag is on the result, not the Config; the README shows tagged Config fields in the app config so each constructor asks for its own.
- [A config that forgets `port` listens on a random port] → Accepted with separate `host` and `port` (decision 8); the README says to set the port explicitly.

## Migration Plan

One pull request. `tls/tlsconfig`, `kernelrun` and `config/configvalidator` are already released, so `http/httpserver` requires their published versions and merging publishes `http/httpserver/v0.1.0`, plus patch releases of `kernel` and `kernelrun` for their README examples. No consumer exists yet.
