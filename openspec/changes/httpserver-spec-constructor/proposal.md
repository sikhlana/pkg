## Why

`ProvideServer`'s constructor builds its own server by calling the Provider it injects, so the only way `ProvideServer` can get the server's name into the build, and into tlsconfig's logs, is to hand that constructor a different Provider. It does that with an `fx.Decorate` that replaces the app's Provider with a fresh one, so an app's own decoration of the Provider (metrics around every server's handler, say) silently doesn't reach any server built through `ProvideServer`. Turning the call around removes the decorator: the constructor says what to serve, and `ProvideServer` builds it through the app's Provider, passing the name as an option.

## What Changes

- **BREAKING** `ProvideServer(name, constructor, anns...)`'s constructor returns a `Spec` (the server's `Config` and `Handler`), optionally followed by an error, instead of a `*Server`. `ProvideServer` wraps the handler in the name's middleware and builds the server by calling the graph's Provider with `WithName(name)`.
- **BREAKING** `Provider` becomes `func(Config, http.Handler, ...Option) (*Server, error)`. `Option` and `WithName(name)` are new: `WithName` puts the name on every record the server logs, tlsconfig's and net/http's included, and works the same without fx.
- A decoration of the app's Provider now applies to every server built through `ProvideServer`. Its handler wrap runs outside the server's middleware, since `ProvideServer` applies the middleware before calling the Provider.
- `ProvideServer` no longer modifies a server after it's built, so its constructor no longer has to return a server of its own.
- `ErrNilServer` is removed: the constructor can't return a nil server any more. A `Spec` with a nil handler fails startup with `ErrNilHandler`, naming the server.
- The `fx.Decorate` inside `ProvideServer` is removed.
- Constitution: a helper that builds named instances builds each one itself, through the module's Provider, passing the name as an option; it never decorates the Provider.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `httpserver`: server constructors return what to serve rather than a server; a nil handler replaces a nil server as the constructor's failure; the name reaches a server's records through the build, under fx or not; and a decoration of the Provider applies to every server.

## Impact

- **`http/httpserver`:** exported API changes: `Spec`, `Option` and `WithName` added; `Provider`'s signature and `ProvideServer`'s constructor contract change; `ErrNilServer` removed. Minor bump to 0.2.0, with `!`. No other module uses it yet.
- **No new dependencies.**
- **Docs and repo files:** httpserver's README (the With fx and Standalone examples, the decoration and middleware-order note, the members table), `docs/http.md` ("Every record names its server" loses its caveat), `openspec/config.yaml` (the named-instance build rule).
- **Release:** one pull request; merging publishes `http/httpserver/v0.2.0`.
