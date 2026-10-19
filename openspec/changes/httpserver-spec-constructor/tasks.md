## 1. Provider options

No mocked interface changes, so there's no go generate task.

- [x] 1.1 `httpserver.go` (now `provider.go` and `options.go`, task 5.1): `Option`, the unexported `options`, `WithName`; `Provider` and `NewProvider` taking `opts ...Option`, building with `logger.With(slog.String("server", name))` when a name is given, per design decision 2.
- [x] 1.2 Tests: a server built by hand with `WithName` names itself on its listening record and on tlsconfig's key-pair record; without it, no `server` attribute.

## 2. Spec constructors

- [x] 2.1 `module.go`: `Spec`; `ProvideServer` providing the constructor's result under `name:"httpserver:spec::built"` (forwarding `anns`), then building through the graph's Provider with the middleware chain and `WithName(name)`; `ErrNilHandler` naming the server for a nil handler; the `fx.Decorate`, `namedProviderParams`, `builtServer` and `ErrNilServer` removed, per design decisions 1 and 3.
- [x] 2.2 Update the existing tests to `Spec` constructors; replace the nil-server test with a nil-handler one.
- [x] 2.3 Tests for the new requirement: an app decoration of the Provider counting requests on two servers; the decoration's wrap seeing a request a middleware answered with 401; the name surviving a decoration that passes `opts...` on.

## 3. Docs and constitution

- [x] 3.1 `http/httpserver/README.md`: members table (`Spec`, `Option`, `WithName`, the new `Provider` and `ProvideServer` rows; `ErrNilServer` removed); the With fx example returning a `Spec`; a `(Spec, error)` example; the Standalone example with `WithName`; a decoration example passing `opts...` on, and the note that its wrap runs outside the middleware.
- [x] 3.2 `openspec/config.yaml`: the named-instance build rule, per design decision 4.
- [x] 3.3 Bump `http/httpserver/VERSION` (minor) to 0.2.0.
- [x] 3.4 `docs/http.md`: rewrite "Every record names its server", which describes the decorator this change removes, as why the module calls the Provider itself and where a decoration's wrap runs; the private tag example follows the new `httpserver:spec::built`. The fuller rollup still happens at archive.

## 4. Verification

- [x] 4.1 In `http/httpserver`: `modernize -fix ./...`, `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), the full `go test ./...` and `go test -race ./...`, then `govulncheck ./...`.
- [x] 4.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 4.1–4.2.

## 5. File per idea

Added during apply, on review: `httpserver.go` had grown to hold the server, the Provider and its options.

- [x] 5.1 Split `httpserver.go` into `server.go`, `provider.go` and `options.go`, and its tests into `server_test.go` and `provider_test.go`.
- [x] 5.2 Move `Spec`, `NameTag` and `ProvideServer` to `server.go`, and `MiddlewareGroup` and `ProvideMiddleware` to `middleware.go`; `module.go` keeps `Module`, the name check and the errors the helpers share.
- [x] 5.3 `openspec/config.yaml`: the file-per-idea rule, and the module layout's `<idea>.go` line in place of `mymodule.go`.
- [x] 5.4 kernel: the flag constants, `ErrMissingFlag`, `flagValues` and `readFlags` to `flags.go`; VERSION to 0.3.2.
- [x] 5.5 log: `Format` and `ErrUnknownFormat` to `format.go`; `MiddlewareGroup`, `WithMiddleware` and `ProvideMiddleware` to `middleware.go`; VERSION to 0.1.2.
- [x] 5.6 telemetry: `telemetry.go` into `resource.go` and `propagator.go`, `telemetry_test.go` into `resource_test.go`, `propagator_test.go` and `sink_test.go`; trace's protocols and exporters to `exporter.go` and its sampler resolvers to `config.go`; VERSION to 0.1.2.
- [x] 5.7 Re-run 4.1 for every touched module, then 4.2 across `go.work`, and the release tool's `check --base origin/main`.

