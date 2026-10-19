## Why

The repo has the pieces a service needs around an HTTP server (a kernel with commands, named runners, operator config, rotating TLS) but no server. The kernel and kernelrun READMEs already show an `httpserver` module in their examples. go-modules has one in `http/server`, but it binds inside its run loop, carries its own certificate reloader, and has grown client-IP resolution and request-id and access-log slots. This change ports the server and its provider as `http/httpserver`, rethought against this repo's constitution: the listener binds at start, TLS comes from `tls/tlsconfig`, and middleware becomes a general per-server group instead of fixed slots.

## What Changes

- New module `http/httpserver` (`github.com/sikhlana/pkg/http/httpserver`), starting at 0.1.0:
  - **Config.** `host` and `port`, the four net/http timeouts and `shutdown_timeout`, each with a default for zero, and an embedded `tlsconfig.ServerConfig` under `tls`.
  - **Server.** Inert once built: no socket, and the TLS files read once to build the `tls.Config`. `Listen`, `Serve` and `Close` split binding, serving and releasing, so the same three steps work standalone and under fx. `Serve` drains in-flight requests for up to `shutdown_timeout` once its context ends. With TLS on, HTTP/2 is negotiated through ALPN. net/http's error log goes to the injected logger.
  - **Provider.** A function from a Config and a handler to a `*Server`, closing over the logger. The one way to build a server under fx.
  - **Named servers only.** `ProvideServer(name, ctor)` provides the `*Server` the constructor builds under the name's tag, wrapped in that name's middleware. Apps list it in shared options, for every command.
  - **The runner owns the lifecycle.** `ProvideRunner(name)` is a command's opt-in: it binds the named server in OnStart, serves it in a runner named `httpserver:<name>`, and closes the listener in OnStop. A command that doesn't list it never builds the server, never binds a socket and never reads its TLS files.
  - **Middleware.** `Middleware` is `ServeHTTP(w, r, next)`, with `next` built once by httpserver for each position in the chain. `OrderedMiddleware` adds `Order() int`; anything else counts as 0. Lower runs outer, and equal orders run in an unspecified order. `MiddlewareFunc` adapts a function, `Ordered` sets or overrides an order at wiring time, `Chain` builds a chain for standalone use, and `ProvideMiddleware(name, ctor)` adds to a named server's group.
  - **Typos fail startup.** A `ProvideMiddleware` or `ProvideRunner` naming no provided server fails with `ErrUnknownServer`, through a check that reads names only and builds nothing.
- Left behind from go-modules: the listener provider and certificate reloader (tlsconfig covers both), trusted proxies and client IP, the request-id and access-log slots, the tagged-config build path and its return-type reflection, the provider's name argument, and a default runner name.
- Constitution: a module with named instances takes the name in `ProvideRunner(name)` and registers its runner as `<module>:<name>`, in place of `DefaultRunnerName`; and a group keyed by an instance name is exposed as a function returning its tag (`MiddlewareGroup(name)`), since it can't be a constant.
- The kernel and kernelrun README examples move to named servers.
- New `docs/http.md`: why the runner owns the lifecycle, and why middleware order comes from `Order()`.

## Capabilities

### New Capabilities

- `httpserver`: building a server touches no socket; a provided server binds and serves only in commands that run it; bind failures are startup failures; graceful shutdown bounded by `shutdown_timeout`; config defaults and validation; TLS through tlsconfig with HTTP/2; middleware order, ties, `next` and per-server groups; unknown names failing startup; net/http's errors reaching the injected logger.

### Modified Capabilities

None. No existing module's behaviour changes.

## Impact

- **`http/httpserver` (new module):** all of its exported API is new. Exposure shape: Provider, for `*Server` built per Config, with `ProvideServer` tagging each one by name. It has a `Module`, a `Config` and a runner (`ProvideRunner(name)`).
- **`kernel`:** README example only; exported API unchanged.
- **`kernelrun`:** README example only; exported API unchanged.
- **New dependencies:** none outside the repo beyond fx. `tls/tlsconfig` and `kernelrun` (both released) and `config/configvalidator` to delegate `tls` validation.
- **Docs and repo files:** `openspec/config.yaml` (the two amendments), `docs/http.md` (new), `README.md` (a Modules row), `go.work` (`use ./http/httpserver`).
- **Release:** one pull request. Merging publishes `http/httpserver/v0.1.0`, `kernel` and `kernelrun` patch releases.
