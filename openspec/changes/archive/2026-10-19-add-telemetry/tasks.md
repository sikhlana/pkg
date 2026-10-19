## 1. Scaffold

The module is new, so it starts at 0.1.0 with no bump task, and no other module's files change. No mocked interface is involved, so there's no go generate task. Nothing is ported from go-modules' `http/requestid`.

- [x] 1.1 Branch `add-telemetry` off `origin/main` after `git fetch`.
- [x] 1.2 Create `telemetry/` with a `go.mod` for `github.com/sikhlana/pkg/telemetry` (go directive from `.go-version`), a `VERSION` containing `0.1.0`, and the packages `telemetry`, `telemetry/trace` and `telemetry/trace/otlpgrpc` laid out as in design decision 1. Done when `go build ./...` passes.
- [x] 1.3 Register `./telemetry` in the root `go.work`.
- [x] 1.4 Add the OpenTelemetry API, SDK and OTLP trace exporters, `go-logr/logr`, `tls/tlsconfig` and `config/configvalidator` at their latest releases, and `go.opentelemetry.io/proto/otlp` for tests. Against those versions, confirm the facts listed in design.md's Context from official docs (explicit options beating the variables, the SDK's sampler env handling, `resource.New` precedence, `FromSlogHandler` levels, the noop tracer's pass-through, the HTTP exporter's `/v1/traces` path). Note any that differ in design.md and adjust before building on them.

## 2. telemetry core

- [x] 2.1 `config.go`: `Config` (endpoint, headers, an embedded `tlsconfig.ClientConfig` under `tls`), its sentinels and `Validate`, per design decision 9.
- [x] 2.2 Resolution: the per-field layer check over operator Config, the environment variables in design decision 2's table, and developer defaults, producing `Settings` with each winning value and its layer. Developer defaults are validated here, failing construction with the same sentinels.
- [x] 2.3 Enablement: an endpoint resolved from some layer and `OTEL_SDK_DISABLED` not `true` (case-insensitive), exposed on `Settings` for the signals, per design decision 3.
- [x] 2.4 Resource: SDK defaults, then `WithServiceName`/`WithServiceVersion` wiring, then `OTEL_RESOURCE_ATTRIBUTES` and `OTEL_SERVICE_NAME`, per design decision 4.
- [x] 2.5 Propagator: the tracecontext and baggage composite as a provided value.
- [x] 2.6 Error sink: the Module's invoke sets `otel.SetErrorHandler` (Warn, error as a typed attribute) and `otel.SetLogger(logr.FromSlogHandler(...))` over the injected logger, per design decision 6.
- [x] 2.7 `log.go`: `LogMiddleware` and `LogMiddlewareOrder`, per design decision 11, without importing `log`.
- [x] 2.8 `module.go`: `Module` (`fx.Module("telemetry")`), `InstallGlobals`, and the `AsX`/`WithX` pairs for defaults, service name and service version, with the tags from design decisions 2 and 4.
- [x] 2.9 Tests for the core scenarios in specs/telemetry/spec.md: the layer table per field (with `t.Setenv`), `OTEL_SDK_DISABLED` values, resource precedence, the propagator round trip, the error sink (a recording `slog.Handler`), the middleware with and without a span, invalid developer defaults failing startup, `Validate` for each invalid case, and globals unchanged unless `InstallGlobals` is listed. Tests touching OpenTelemetry's globals don't run in parallel and restore them in `t.Cleanup`.

## 3. telemetry/trace

- [x] 3.1 `config.go`: `Config` (sampler, sampler_arg), its sentinels and `Validate`, with the sampler and its argument resolving as one unit, per design decisions 2 and 9.
- [x] 3.2 Provider construction: the noop provider when disabled; otherwise `sdktrace` with the resource, a batch processor, the sampler passed explicitly only when operator config or developer defaults won, and the HTTP exporter given only the options whose fields didn't resolve from the environment. TLS from operator config or defaults goes through `tlsconfig.ClientConfig.TLSConfig`. The `TRACES_` variables are re-checked here.
- [x] 3.3 Exporter wiring: `ExporterProvider`, `WithExporter` recording the protocol, and the protocol check against `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`/`OTEL_EXPORTER_OTLP_PROTOCOL` when enabled, with `ErrUnsupportedProtocol`, per design decision 7.
- [x] 3.4 Supplied provider: `WithTracerProvider`/`AsTracerProvider` under `telemetry/trace:provider:supplied`; used unchanged, no exporter, no shutdown, a Warn when an endpoint also resolved, per design decision 8.
- [x] 3.5 Lifecycle: OnStop shuts the built provider down with the hook's context, logging failures at Warn and returning nil, per design decision 10.
- [x] 3.6 `module.go`: `Module` (`fx.Module("telemetry/trace")`) and the `AsX`/`WithX` pair for trace defaults.
- [x] 3.7 `otlpgrpc`: `New` as an `ExporterProvider` over `otlptracegrpc`, honouring the same layering, and `ProvideExporter`.
- [x] 3.8 Tests for the trace scenarios in specs/telemetry/spec.md, under `fxtest` with the real Modules: export to an `httptest` OTLP collector and an in-process gRPC trace service on 127.0.0.1:0; headers; TLS with a private CA and an untrusted one (CA and leaf fixtures written into `t.TempDir()`); every sampler name and the parent-based default; the protocol check, including `http/json` exporting JSON and the disabled case; pass-through when disabled; a supplied provider; nothing listening; flush on stop.

## 4. Constitution, lint and docs

- [x] 4.1 In `openspec/config.yaml`: a group directory may be a module; packages inside a module are named for their role, with their repo-relative path (`telemetry/trace`) as fx.Module name and tag prefix; the `OTEL_*` exception to configloader; providers and propagators taken by injection.
- [x] 4.2 In `.golangci.yml`, ban `otel.Tracer`, `otel.Meter`, `otel.GetTracerProvider`, `otel.GetMeterProvider` and `otel.GetTextMapPropagator` through forbidigo, with a message naming injection.
- [x] 4.3 Write `docs/telemetry.md` from the rollup list in design.md.
- [x] 4.4 Write `telemetry/README.md` in the constitution's order: overview; Configuration for both Configs, with the layer table and the variables each field reads; Exported Members for all three packages; Usage, Standalone and With fx, for a service (configloader) and a CLI tool (defaults), registering `LogMiddleware`, `InstallGlobals` and what breaks without it, gRPC, a supplied provider, and the stop timeout. No backticks in table cells.
- [x] 4.5 In the root `README.md`, add the `telemetry` row to the Modules table in alphabetical order.

## 5. Verification

- [x] 5.1 In `telemetry/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`, plus `go test -race ./...`.
- [x] 5.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 5.1–5.2.

## 6. Code review fixes

See design.md's Fixes from code review. Tasks 2.2, 2.3, 2.8, 3.2 and 3.6 built developer defaults, which these remove.

- [x] 6.1 Remove developer defaults: `Settings`, `Resolved`, `Layer`, `NewSettings`, `SettingsParams` and the `AsDefaults`/`WithDefaults` pairs in both packages. `Config.Enabled(signal)` decides enablement; `trace.Params` and `ExporterProvider` take `telemetry.Config`.
- [x] 6.2 Build the exporter and register the batch processor in `trace.New`'s OnStart hook; the constructor builds a provider with no processor.
- [x] 6.3 TLS only beside an https endpoint: `ErrTLSWithoutHTTPS` in `Config.Validate`, and `Config.UseTLS` in both exporters.
- [x] 6.4 Validate header values with `ErrInvalidHeaderValue`, keeping the value out of the error.
- [x] 6.5 Replace the hand-rolled endpoint check and `ErrInvalidEndpoint` with an `omitempty,http_url` validate tag.
- [x] 6.6 Keep a partial resource on `resource.ErrPartialResource`, logging a warning; add `ResourceParams.Logger`.
- [x] 6.7 Return the sampler's ratio error from `trace.Config.sampler` and fail `New` on it.
- [x] 6.8 Scope the error sink to the app's run: a forwarding handler and logger over an atomically swapped logger, `InstallErrorSink` returning an uninstall function, and `telemetry.Module` installing on start and uninstalling on stop. Test the forwarder with `testing/slogtest`.
- [x] 6.9 Tests for each fix, and the spec, README, `docs/telemetry.md` and the constitution's `OTEL_*` paragraph updated to match.
- [x] 6.10 Re-run 5.1 and 5.2.

## 7. Second code review fixes

See design.md's Fixes from the second code review.

- [x] 7.1 Drop `InstallGlobals`' restore on stop and document the globals as process-wide.
- [x] 7.2 Honour `OTEL_TRACES_EXPORTER` in `Config.Enabled`: `none` off, `otlp` or unset on, anything else `ErrUnsupportedExporter`.
- [x] 7.3 Fail on a malformed endpoint variable with `ErrInvalidEndpointVariable`; `Config.Enabled` returns `(bool, error)`.
- [x] 7.4 Warn about unused settings for a supplied provider only for trace-specific settings.
- [x] 7.5 Document that errors from OpenTelemetry's internal logger log at Error.
- [x] 7.6 Merge the sampler's validation and construction into one switch.
- [x] 7.7 Move the provider half of `TestInstallGlobals` to the trace package, dropping `fx.Annotate`.
- [x] 7.8 Tests for each fix; spec, README and `docs/telemetry.md` updated.
- [x] 7.9 Re-run 5.1 and 5.2.

## 8. Third code review fixes

See design.md's Fixes from the third code review.

- [x] 8.1 Correct the forbidigo message for the global setters.
- [x] 8.2 Read `OTEL_TRACES_EXPORTER` as a trimmed, lowercased list, and compare the protocol variables trimmed and lowercased.
- [x] 8.3 Check only the endpoint variable the exporter will read.
- [x] 8.4 Add `Config.Plaintext` and turn TLS off explicitly in both exporters for an http Config endpoint.
- [x] 8.5 Fall back to a standard-error handler for errors and warnings while no logger is installed; make a nil logger discard; build the error logger once per install.
- [x] 8.6 Tests for each fix, with the plaintext tests checked against the unfixed code; test helpers install a discarding sink so restoring globals stays quiet.
- [x] 8.7 README, `docs/telemetry.md`, spec and design.md updated.
- [x] 8.8 Re-run 5.1 and 5.2.
