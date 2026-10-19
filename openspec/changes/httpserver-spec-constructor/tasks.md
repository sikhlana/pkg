## 1. Provider options

No mocked interface changes, so there's no go generate task.

- [ ] 1.1 `httpserver.go`: `Option`, the unexported `options`, `WithName`; `Provider` and `NewProvider` taking `opts ...Option`, building with `logger.With(slog.String("server", name))` when a name is given, per design decision 2.
- [ ] 1.2 Tests: a server built by hand with `WithName` names itself on its listening record and on tlsconfig's key-pair record; without it, no `server` attribute.

## 2. Spec constructors

- [ ] 2.1 `module.go`: `Spec`; `ProvideServer` providing the constructor's result under `name:"httpserver:spec::built"` (forwarding `anns`), then building through the graph's Provider with the middleware chain and `WithName(name)`; `ErrNilHandler` naming the server for a nil handler; the `fx.Decorate`, `namedProviderParams`, `builtServer` and `ErrNilServer` removed, per design decisions 1 and 3.
- [ ] 2.2 Update the existing tests to `Spec` constructors; replace the nil-server test with a nil-handler one.
- [ ] 2.3 Tests for the new requirement: an app decoration of the Provider counting requests on two servers; the decoration's wrap seeing a request a middleware answered with 401; the name surviving a decoration that passes `opts...` on.

## 3. Docs and constitution

- [ ] 3.1 `http/httpserver/README.md`: members table (`Spec`, `Option`, `WithName`, the new `Provider` and `ProvideServer` rows; `ErrNilServer` removed); the With fx example returning a `Spec`; a `(Spec, error)` example; the Standalone example with `WithName`; a decoration example passing `opts...` on, and the note that its wrap runs outside the middleware.
- [ ] 3.2 `openspec/config.yaml`: the named-instance build rule, per design decision 4.
- [ ] 3.3 Bump `http/httpserver/VERSION` (minor) to 0.2.0.

## 4. Verification

- [ ] 4.1 In `http/httpserver`: `modernize -fix ./...`, `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), the full `go test ./...` and `go test -race ./...`, then `govulncheck ./...`.
- [ ] 4.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 4.1–4.2.
