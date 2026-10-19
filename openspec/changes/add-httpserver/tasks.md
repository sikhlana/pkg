## 1. Scaffold

The module is new, so it starts at 0.1.0 with no bump task. No mocked interface is involved, so there's no go generate task. Nothing from go-modules' `http/server` is copied: each block is rewritten, with fresh godoc and comments.

- [x] 1.1 Branch `add-httpserver` off `origin/main` after `git fetch`.
- [x] 1.2 Create `http/httpserver/` with a `go.mod` for `github.com/sikhlana/pkg/http/httpserver` (go directive from `.go-version`), a `VERSION` containing `0.1.0`, and the files laid out as in design decision 1; register `./http/httpserver` in the root `go.work`. Add fx, `tls/tlsconfig`, `kernelrun` and `config/configvalidator` at their latest releases, and testify. Done when `go build ./...` passes.
- [x] 1.3 Confirm the facts in design.md's Context against the Go release in `.go-version` and the libraries' official docs: `ServeTLS` with empty file names, its ALPN on a clone, and the default `Protocols`; `Shutdown`'s behaviour on timeout; `slog.NewLogLogger`; and whether `hostname_port` accepts an empty host and port 0. Note any that differ in design.md and adjust before building on them.

## 2. Config

- [x] 2.1 `config.go`: `Config` with `host`, `port`, the five timeouts and `tls` (`tlsconfig.ServerConfig`), the `Default*` constants and an unexported resolver for each timeout, per design decision 8.
- [x] 2.2 `Validate`: `ErrNegativeTimeout` for each negative timeout, naming its key, joined with `configvalidator.ValidateField(&c, &c.TLS)`; `host` and `port` checked by their tags (decision 8).
- [x] 2.3 Tests for the config scenarios in specs/httpserver/spec.md: defaults for zero timeouts, a malformed host and an out-of-range port, each negative timeout, and an incomplete TLS section, including loading through configloader.

## 3. Server and Provider

- [x] 3.1 `httpserver.go`: `Server` with the states in design decision 4 (`Listen`, `Serve`, `Close`, `Addr`, `ErrNotListening`, `ErrAlreadyListening`), a mutex, pointer receivers.
- [x] 3.2 `Serve` and shutdown per design decision 5: `Serve` or `ServeTLS(l, "", "")`, the draining goroutine bounded by `shutdown_timeout` on `context.WithoutCancel`, `Close` and the wrapped `context.DeadlineExceeded` when the drain runs out of time, nil after a clean drain, and other serve errors wrapped.
- [x] 3.3 `Provider`, `ProviderParams` and `NewProvider`: build the `tls.Config` through `ServerConfig.TLSConfig` with the logger, fail on a nil handler with `ErrNilHandler`, set the four net/http timeouts and `ErrorLog` through `slog.NewLogLogger` at Debug, per design decisions 2 and 9.
- [x] 3.4 Logging per design decision 9: listening (with the bound address), draining and stopped.
- [x] 3.5 Tests for the scenarios under "Building a server binds nothing", "Graceful shutdown bounded by shutdown_timeout", "TLS through tlsconfig, with HTTP/2" and "Logging through the injected logger": servers built directly with `127.0.0.1:0`; TLS with a private CA and leaf written into `t.TempDir()`; ALPN checked from the client's connection state; rotation with a short reload interval; a slow handler for the drain cases; a recording `slog.Handler`, and the standard library's log output captured to prove it stays empty.

## 4. Middleware

- [x] 4.1 `middleware.go`: `Middleware`, `OrderedMiddleware`, `MiddlewareFunc`, `Ordered` and `Chain`, per design decision 6: stable ascending sort, undeclared orders as 0, each `next` built once.
- [x] 4.2 Tests for the ordering scenarios through `Chain`: lower orders outer, undeclared as 0, `Ordered` overriding a declared order, equal orders both running, short-circuiting, and the same `next` across concurrent requests (run with `-race`).

## 5. fx wiring

- [x] 5.1 `module.go`: `Module` (`fx.Module("httpserver")`) providing the Provider and invoking the name check; `NameTag`, `MiddlewareGroup`; `ProvideServer(name, ctor)` providing the constructor's result privately, applying the name's middleware group through `Chain`, naming the server's logger, providing it under `NameTag(name)` and supplying the name into the server-names group; `ProvideMiddleware(name, ctor)` with `fx.As(new(Middleware))` and the group tag, supplying the name into the targets group; `ErrUnknownServer`; per design decisions 2 and 7.
- [x] 5.2 `runner.go`: `ProvideRunner(name)` through `kernelrun.ProvideRunner("httpserver:"+name, ...)`, taking the named server as optional and returning `ErrUnknownServer` when it's missing, and appending OnStart `Listen` and OnStop `Close`, per design decision 3.
- [x] 5.3 Tests under `fxtest` with the real Module and `fx.Supply` for config, handlers and logger, plus `kernelrun.Run` for the command-level cases: two servers with one run and one not; a command running neither building nothing (constructors and middleware constructors counting their calls, a TLS server with a missing key file); an injected but unrun server binding nothing; the address already taken failing startup with nothing run; a later start hook failing and the socket being released; one server's failure stopping the other and naming `httpserver:<name>`; middleware staying with its server; and `ErrUnknownServer` for a middleware and a runner name, with the check building nothing.

## 6. Constitution and docs

- [ ] 6.1 In `openspec/config.yaml`, the two amendments in design decision 10: `ProvideRunner(name)` registering as `<module>:<name>` for named instances, and per-name groups exposed as a function returning the tag.
- [ ] 6.2 Write `docs/http.md` from the rollup list in design.md.
- [ ] 6.3 Write `http/httpserver/README.md` in the constitution's order: overview; Configuration; Exported Members; Usage, Standalone (Listen, Serve, Close, and `Chain`) and With fx (servers in `kernel.Options.Shared`, `ProvideRunner` per command, tagged Config fields through `fx.Out`, middleware with and without an order, adapting a `func(http.Handler) http.Handler` such as otelhttp's, recommending a recovery middleware at a low order, and keeping `shutdown_timeout` below the orchestrator's grace period). No backticks in table cells.
- [ ] 6.4 In the root `README.md`, add the `httpserver` row to the Modules table in alphabetical order.
- [ ] 6.5 Update the examples in `kernel/README.md` and `kernelrun/README.md` to named servers: `httpserver.ProvideServer` in shared options and `httpserver.ProvideRunner("api")` on the command; drop `DefaultRunnerName` from kernelrun's.
- [ ] 6.6 Bump `kernel/VERSION` (patch) for its README edit.
- [ ] 6.7 Bump `kernelrun/VERSION` (patch) for its README edit.

## 7. Verification

- [ ] 7.1 In `http/httpserver/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...` plus `go test -race ./...`, then `govulncheck ./...`.
- [ ] 7.2 In `kernel/` and `kernelrun/`: the same chain, since their README edits are their only changes.
- [ ] 7.3 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 7.1–7.3.
