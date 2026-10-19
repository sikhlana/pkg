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

### 5. One file per idea (added during apply)

Review found `httpserver.go` crowded once the Provider gained options, so the package now splits by idea: `server.go` (the Server, `Spec`, `NameTag`, `ProvideServer`), `provider.go` (`Provider`, `ProviderParams`, `NewProvider`, `ErrNilHandler`), `options.go` (`Option`, `WithName`), `middleware.go` (gaining `MiddlewareGroup` and `ProvideMiddleware`) and `runner.go`. `module.go` keeps `Module`, the name check and the errors shared across helpers. An idea's `ProvideX` lives with the idea rather than in `module.go`, and a Params struct stays beside its constructor; the constitution gains the rule. No exported API changes. A scan of the other modules found four places out of line with the rule, and this branch fixes them too, each a refactor with a patch bump and no API change: kernel's flags move to `flags.go`; log's `Format` moves to `format.go` and its `MiddlewareGroup`, `WithMiddleware` and `ProvideMiddleware` from `module.go` to `middleware.go`; telemetry's `telemetry.go` becomes `resource.go` and `propagator.go`; and telemetry/trace's protocols and exporters move to `exporter.go`, with the `Config` sampler resolvers going to `config.go` beside `Sampler`. Tests follow where a test file mixed ideas (httpserver's, and telemetry's resource, propagator and error-sink tests); tests that exercise a package through its main entry point (kernel's App, trace's `New`) stay where they are. kernelrun already fits: `kernelrun.go` is `Run`, and `runner.go` is the `Runner` with its helpers.

### 6. The rule reworded, and applied everywhere (added during review)

Review found the first wording ambiguous and the split incomplete. The constitution now says: `<package>.go` holds the entry point (New, Run, Load) or only the doc; an idea's `ProvideX`, `WithX` and `AsX` live in `x.go` when it exists, and a small idea's helpers stay in `module.go`; every exported sentinel lives in `errors.go` (a single-file package excepted), so failure modes are listed in one place and an error doesn't move when a second idea uses it; and tests follow the file they test, with `helpers_test.go`/`export_test.go` for plumbing and end-to-end entry-point tests in `<package>_test.go`. Applied to every module and to tools/release, each a pure move checked by comparing every non-import line before and after (tlsconfig's Validate tests, which shared subtests with the TLSConfig tests, were split out with every assertion kept). `url/urlmask` was already in line. A mechanical scan afterwards found no sentinel outside `errors.go`, no helper in `module.go` whose idea has a file, and no test file without its source but the two generated mocks.

## Fixes from code review

A high-effort review of the branch found these; each fix has a test that fails without it:

- **A typed-nil handler got past the nil check.** `spec.Handler == nil` missed a nil `*Handler` or nil `HandlerFunc` behind the interface, starting a server that panics on every request (logged only at Debug). `ProvideServer` and the Provider now use `isNil`, the reflection check `Chain` already used for middleware.
- **"A decoration applies to every server" overstated it.** fx scopes `fx.Decorate` to the declaring module and the modules inside it, so a decoration in a sibling module (a `metrics` module beside `api.Module`) reaches no server and reports nothing. The godoc, README and `docs/http.md` now say to declare it at the root or in an enclosing module, and tests pin both cases. The spec's requirement says the same.
- **Only the nil-handler failure named the server.** Every error from building through the Provider is now wrapped as `building server %q: %w`, so a missing key file on `admin` says so.
- **A nil `Option` panicked** inside `NewProvider`; it's skipped.
- **A constructor returning `*Spec`** failed with a missing type naming the private tag; the README says to return a value.

## Open after apply

- One server can't be built differently from the others (its own logger, say): the constructor no longer touches the Provider. A `Spec.Options []Option` field, appended after `WithName`, would allow it; left until someone needs it.
- configvalidator and telemetry have no package doc; the rule's `<package>.go` would carry one when written.

## Risks / Trade-offs

- [The breaking change] → httpserver is at 0.1.0 with no known consumer; a minor bump with `!`.
- [A decoration that drops `opts...` loses the server's name on its records] → The README's decoration example passes them on; the spec states the requirement on the decoration's side.
- [A decoration's wrap sees requests the middleware answered] → Intended (decision 3), and documented.

## Migration Plan

One pull request; merging publishes `http/httpserver/v0.2.0`. A consumer's constructor changes from calling `p(cfg, h)` to returning `httpserver.Spec{Config: cfg, Handler: h}`, and drops the Provider from its inputs.
