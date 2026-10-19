# HTTP servers

[http/httpserver/README.md](../http/httpserver/README.md) says how to declare, run and wrap a server. This page says why the module is shaped that way. The full discussion, including the alternatives rejected, is in the add-httpserver change's design.md under [openspec/changes](../openspec/changes).

## The runner owns the lifecycle

A server goes through three steps: bind, serve, release. `Server` exposes them as `Listen`, `Serve` and `Close`, and building one does none of them. Under fx, `ProvideRunner(name)` does all three: it appends an OnStart hook that calls `Listen`, registers `Serve` as the kernelrun runner `httpserver:<name>`, and appends an OnStop hook that calls `Close`. The Provider that builds the server appends nothing.

Binding in OnStart, not in `Serve`, follows the constitution's rule for loops: setup that can fail (a port someone else holds) fails startup, before any runner runs, and an app that finished starting is accepting connections. go-modules bound inside `Serve`, so a port conflict surfaced as a runner crash after startup had already reported success.

Keeping the hooks out of the Provider matters because injecting a server isn't running it. A command might inject a server to read its config or decorate it without serving it. If building the server appended the hooks, that command would bind a socket nobody serves: the kernel accepts connections into the backlog and clients hang until they time out. With the runner owning the hooks, a server binds only in the commands that list `ProvideRunner` for it.

`Close` only releases a listener that `Serve` never took over. That's the case where a later start hook fails: fx rolls back by running the OnStop hooks of what already started, and the socket has to be freed though no runner ran. Once `Serve` has the listener, it drains and closes it itself, and `Close` is a no-op.

Every server is named and there's no default runner name. The prefix keeps a server named `migrate` from colliding with another module's runner of that name.

## Checking names without building anything

`ProvideMiddleware` and `ProvideRunner` name a server as a string, so a typo would otherwise do nothing: middleware that never runs, or a command that serves nothing. Both fail startup with `ErrUnknownServer` instead, without building anything a command doesn't use.

`ProvideServer(name)` and `ProvideMiddleware(name)` each supply `name` into an unexported string group, and `Module` invokes a check over those two groups alone. Supplied strings cost nothing to build, so the check runs in every command without constructing a server, a middleware or a TLS configuration, and a command that doesn't use a server never reads its key files. `ProvideRunner`'s constructor takes the named server as optional and returns `ErrUnknownServer` when it's missing; it needs the server anyway, so no separate check is needed there.

## Middleware order comes from the middleware

A server's middleware is a value group per server, and dig doesn't guarantee the order of a group's members: it shuffles them. So registration order can't decide anything, and the order lives on the middleware itself. `OrderedMiddleware` adds `Order() int`, lower runs outer, and anything without it counts as 0. An app that wants to place middleware it didn't write wraps it with `Ordered(n, m)`.

Equal orders don't fail startup. Most middleware doesn't care where it sits, and failing on every tie would force an order onto all of it. A tie-break by type name was rejected too: it would hide the shuffle without making the order meaningful. The middleware that does care declares an order or is given one.

## Why next is a parameter

`Middleware` is `ServeHTTP(w, r, next)` rather than the ecosystem's `func(http.Handler) http.Handler`. httpserver builds each position's `next` once, when it builds the chain, so it's the same handler for every request and nothing in the chain is rebuilt per request. The signature reads like net/http's own, and a type can't be both an `http.Handler` and a `Middleware`, since the two `ServeHTTP` methods clash, which stops a handler being registered as middleware by mistake.

Ecosystem middleware still adapts through `MiddlewareFunc`, calling the wrapper per request. otelhttp's `NewMiddleware` does its setup once and returns a wrapper that only allocates a closure, so that costs one small allocation per request, not enough to justify a second middleware shape.
