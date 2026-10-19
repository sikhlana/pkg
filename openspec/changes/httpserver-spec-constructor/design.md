## Context

See proposal.md for why. The requirements are in specs/httpserver/spec.md. The shape was worked out in conversation after `add-httpserver` was archived: its follow-up "a decoration of the app's Provider doesn't reach servers built through `ProvideServer`" turned out to come from who calls the Provider, not from where the TLS configuration is built.

Today:

```go
// ProvideServer's module: replaces the constructor's Provider with a named one.
fx.Decorate(func(p namedProviderParams) Provider {
	return NewProvider(ProviderParams{Logger: p.Logger.With(slog.String("server", name))}) // p.Provider discarded
}),
fx.Provide(fx.Private, fx.Annotate(constructor, fx.ResultTags(builtServer))),
fx.Provide(fx.Annotate(func(s *Server, mws []Middleware) (*Server, error) {
	s.handler = Chain(s.handler, mws...) // modifies the built server in place
	...
}, ...)),
```

## Goals / Non-Goals

**Goals:**

- No `fx.Decorate` in the module; an app's decoration of the Provider applies to every server.
- The name reaches every record, tlsconfig's included, with the TLS configuration still built (and a bad key pair still failing) at build time.
- The server's name is written once, in `ProvideServer`.

**Non-Goals:**

- Middleware that applies to every server. A Provider decoration can do it; a dedicated group can come when someone needs one.
- Building the TLS configuration in `Listen` instead. Considered and not needed once the Provider takes the name.

## Decisions

### 1. The constructor returns a `Spec`

```go
type Spec struct {
	Config  Config
	Handler http.Handler
}

httpserver.ProvideServer("api", func(c api.Config, h *api.Handler) httpserver.Spec {
	return httpserver.Spec{Config: c.Config, Handler: h}
})
```

`ProvideServer` provides the constructor's result privately under `name:"httpserver:spec::built"`, then builds:

```go
func(spec Spec, p Provider, mws []Middleware) (*Server, error) {
	if spec.Handler == nil {
		return nil, fmt.Errorf("%w: server %q", ErrNilHandler, name)
	}

	return p(spec.Config, Chain(spec.Handler, mws...), WithName(name))
}
```

The nil-handler check comes before `Chain`, which would otherwise wrap a nil handler and fail per request. `(Spec, error)` stays allowed for constructors that fail building their handler; fx handles a trailing error itself.

**Alternatives:** the constructor keeps returning a `*Server` and `ProvideServer` keeps a decorator, made to wrap rather than replace (it composes, but every named-instance module keeps a decorator); building the TLS configuration in `Listen` so the name can be set after the build (removes the decorator too, but moves a bad key pair's failure from build to start, and a decoration still can't influence a build it didn't make); go-modules' constructor returning a `Config` and an `http.Handler` in either order, found by reflection (the reflection and its panics are why `add-httpserver` rejected it; a struct says the same thing with types). The name `Spec` over `Definition` or `ServerSpec`: `httpserver.Spec` reads as what the constructor specifies.

### 2. `Provider` takes options

```go
type Provider func(cfg Config, handler http.Handler, opts ...Option) (*Server, error)

type Option func(*options)

func WithName(name string) Option
```

`options` is unexported, holding the name; `WithName` makes the Provider build with `logger.With(slog.String("server", name))`, so the server's records, net/http's error log and tlsconfig's key-pair records carry it. An empty name adds nothing. A functional option over a third positional parameter: the Provider's next per-build setting won't need another signature change, and a decoration passes `opts...` through without knowing what's in them.

### 3. Middleware wraps the handler before the build

Applying the middleware to `Spec.Handler` before calling the Provider means the built server is never modified, and a decoration that wraps the handler wraps the whole chain: its wrap runs outside the middleware. A decoration applies to every server, so outermost is where it belongs; middleware that has to run inside recovery or auth is a middleware with an order, not a decoration. The README says so.

### 4. The constitution rule

"A helper that builds named instances builds each one itself, through the module's Provider, passing the name as an option; it never decorates the Provider." It goes beside the forwarding rule, which still applies: `ProvideServer` forwards annotations to the constructor that returns the `Spec`.

### Rollup at archive

Decisions 1–3 into `docs/http.md`, replacing "Every record names its server"'s caveat with why the module calls the Provider itself.

Brought forward during apply (task 3.4): that section described the decorator this change removes, so it would have been wrong once merged. It's now "The module builds every server itself", and the private-tag example in "Checking names without building anything" reads `httpserver:spec::built`. Archive still checks the page against what shipped.

## Risks / Trade-offs

- [The breaking change] → httpserver is at 0.1.0 with no known consumer; a minor bump with `!`.
- [A decoration that drops `opts...` loses the server's name on its records] → The README's decoration example passes them on; the spec states the requirement on the decoration's side.
- [A decoration's wrap sees requests the middleware answered] → Intended (decision 3), and documented.

## Migration Plan

One pull request; merging publishes `http/httpserver/v0.2.0`. A consumer's constructor changes from calling `p(cfg, h)` to returning `httpserver.Spec{Config: cfg, Handler: h}`, and drops the Provider from its inputs.
