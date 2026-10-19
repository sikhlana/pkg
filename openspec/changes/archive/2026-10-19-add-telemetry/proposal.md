## Why

Services and tools built on this repo have no way to follow one unit of work across processes, or to tie a log line to the request that produced it. go-modules solved part of that with `http/requestid`, a hand-rolled correlation id carried in `X-Request-ID`. OpenTelemetry already solves all of it: propagation between services, root spans for background work, ids on every log record, and backends that understand them. So `requestid` isn't ported. This change adds a `telemetry` module that sets OpenTelemetry up the way every other module here is set up: wired through fx, configured by the operator for a service or by the developer for a CLI tool, and silent unless something configures it.

## What Changes

- New module `telemetry` (`github.com/sikhlana/pkg/telemetry`), starting at 0.1.0. The core package stitches the signals together through the fx graph; each signal is a subpackage that imports the core, never the other way round:
  - **Resource.** `service.name` and `service.version` come from developer wiring (`WithServiceName`, `WithServiceVersion`). `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` override them, the same rule as config: an operator can rename a deployment without a rebuild.
  - **Propagator.** W3C tracecontext and baggage, provided as a graph value so explicitly declared middleware can use it without reading OpenTelemetry's globals.
  - **Shared OTLP settings, Config over the environment.** Each setting comes from the app's Config when it's set there, and from the `OTEL_*` environment variables when it isn't. A service's Config comes from configloader; a CLI tool supplies one in code, or none and leaves it to `OTEL_*`. (A third layer of developer defaults below the environment was dropped in code review; see design.md.)
  - **Off unless configured.** With no OTLP endpoint from the Config or the environment, or with `OTEL_SDK_DISABLED=true`, nothing is exported and the signals get noop providers. A CLI tool never phones home by default.
  - **TLS to the collector** comes from an embedded `tls/tlsconfig` client config, so CA and key pair files rotate without a restart. The endpoint's scheme picks plaintext (`http://`) or TLS (`https://`).
  - **OpenTelemetry's own errors and logs** go to the app's injected `*slog.Logger`. That sink is process-wide and installed whenever the module is listed: the SDK reports export failures through nothing else.
  - **Log correlation.** A function matching `log.Middleware`'s standard-library signature adds `trace_id` and `span_id` to every record logged with a context carrying a valid span. `telemetry` doesn't import `log`; the app registers it.
  - **Globals are opt-in.** Listing the modules only provides values. A separate helper installs the tracer provider and propagator as OpenTelemetry's globals, for third-party libraries that can only read them.
- New subpackage `telemetry/trace`: provides a `trace.TracerProvider` built from the shared Config, exporting OTLP over HTTP/protobuf, with a spec-shaped sampler (`sampler`, `sampler_arg`) that wins over `OTEL_TRACES_SAMPLER` the same way. The exporter is built when the app starts, so an app that fails to start leaves nothing running. A subpackage, `telemetry/trace/otlpgrpc`, swaps in OTLP over gRPC, so only apps that ask for gRPC build it. The protocol is developer wiring; an `OTEL_EXPORTER_OTLP_PROTOCOL` naming one the app didn't link fails startup. An app can supply its own `TracerProvider` instead; the module then uses it as-is and leaves its lifecycle to the app. A provider the module built is flushed and shut down when the app stops.
- An unreachable collector never stops or fails the app. Invalid configuration fails startup.
- Constitution:
  - A group directory may itself be a module, and packages inside a module are named for their role (`telemetry/trace`), not `<group><name>`. The `<group>/<group><name>` rule stays for nested modules.
  - The `OTEL_*` environment variables are the documented exception to "operator config goes through configloader".
  - Modules take `trace.TracerProvider` and `propagation.TextMapPropagator` by injection. `.golangci.yml` bans `otel.Tracer`, `otel.Meter`, `otel.GetTracerProvider`, `otel.GetMeterProvider` and `otel.GetTextMapPropagator` with forbidigo.
- New `docs/telemetry.md`: why OpenTelemetry instead of a request id, Config over `OTEL_*`, why the environment bypasses configloader, and why developer defaults were dropped, why no endpoint means off (unlike the OTLP spec's localhost default), why globals are opt-in while the error handler isn't, and how the provider seam works.
- The root README gains a Modules row, and `go.work` registers the module.

## Capabilities

### New Capabilities

- `telemetry`: resource identity and its overrides; Config winning over the environment for the shared OTLP settings and the sampler; off unless an endpoint is configured, and `OTEL_SDK_DISABLED`; the provided propagator; tracer provider export over OTLP HTTP or gRPC; app-supplied providers and who shuts what down; flushing on stop; startup failing on invalid config but never on an unreachable collector; OpenTelemetry's errors reaching the injected logger; opt-in globals; and the log middleware's attributes.

### Modified Capabilities

None. No existing module's behaviour changes.

## Impact

- **`telemetry` (new module):** all of its exported API is new. Exposure shapes: singletons. The core `Module` provides the resource and the propagator; `trace.Module` provides the `TracerProvider`; `otlpgrpc` contributes an exporter choice as developer wiring. Both packages have a Config (`telemetry.Config` for the shared OTLP endpoint, headers and TLS; `trace.Config` for the sampler). There's no runner: the batch span processor's goroutine is the SDK's, started with the provider and stopped in OnStop.
- **Other modules:** none touched. `log` is integrated through its existing `Middleware` signature, without either importing the other. A future HTTP server or client takes the provider and propagator by injection and declares its middleware explicitly; that's out of scope here.
- **New dependencies:** the OpenTelemetry Go API and SDK (`go.opentelemetry.io/otel`, `otel/trace`, `otel/sdk`), the OTLP trace exporters (`otlptracehttp`, and `otlptracegrpc` with grpc, imported only by `otlpgrpc`), and `github.com/go-logr/logr` for `FromSlogHandler`. `tls/tlsconfig` (already released) for the exporter's TLS settings, and `config/configvalidator` to delegate its validation. `go.opentelemetry.io/proto/otlp` is required by tests only.
- **Docs and repo files:** `openspec/config.yaml` (the layout, `OTEL_*` and injection amendments), `.golangci.yml` (the forbidigo bans), `docs/telemetry.md` (new), `README.md` (a Modules row), `go.work` (`use ./telemetry`).
- **Release:** one pull request. Merging publishes `telemetry/v0.1.0`.
