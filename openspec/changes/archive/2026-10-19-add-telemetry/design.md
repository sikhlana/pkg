## Context

See proposal.md for why OpenTelemetry replaces go-modules' `http/requestid`. The requirements are in specs/telemetry/spec.md. This document covers how they're met and what was rejected. Datadog (dd-trace-go, Orchestrion) is out of scope; the provider seam in decision 8 keeps a vendor bridge possible later without designing for one now.

Facts that shape the approach, confirmed against OpenTelemetry Go v1.47.0 in task 1.4:

- **The Go SDK doesn't implement `OTEL_SDK_DISABLED`.** The maintainers declined it (opentelemetry-go#3559) because Go apps set the SDK up explicitly. The spec says only the value `true` disables; anything else has no effect.
- **The SDK has no "disabled" mode of its own.** Disabled means handing out the API's noop providers.
- **In the OTLP exporters, an explicit option beats the matching `OTEL_EXPORTER_OTLP_*` variable**, and an unset option leaves the exporter to read the variable itself. Per-signal variables (`…_TRACES_ENDPOINT`) beat generic ones.
- **`sdktrace.NewTracerProvider` reads `OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG` only when no `WithSampler` option is passed.**
- **`resource.WithFromEnv` reads `OTEL_RESOURCE_ATTRIBUTES` and `OTEL_SERVICE_NAME`**, the latter winning over a `service.name` inside the former, and in `resource.New` a later detector's attribute wins over an earlier one's.
- **The SDK's batch processor and exporters report failures only through `otel.Handle`**, the global error handler, and log through the global `logr.Logger`. There's no per-provider option.
- **`logr.FromSlogHandler` maps logr verbosity `V(n)` to slog level `-n`.** OpenTelemetry logs informational messages at `V(4)` and debug at `V(8)`, so they land at or below `slog.LevelDebug`.
- **A noop tracer's `Start` returns a non-recording span carrying the parent's span context** from the context, so propagation passes through a disabled tracer.
- **The HTTP exporter speaks `http/protobuf` and `http/json`.** Without `WithEncoding` it reads the encoding from `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` or `OTEL_EXPORTER_OTLP_PROTOCOL`. (The proposal assumed protobuf only; v1.47.0 has both.)
- **`WithEndpointURL` uses the URL's path as-is**, unlike the endpoint variables, which have `/v1/traces` appended. So the module appends `/v1/traces` itself to an endpoint from operator config or developer defaults.

## Goals / Non-Goals

**Goals:**

- One way to set telemetry up for both a service (operator config) and a CLI tool (developer defaults), with `OTEL_*` working in both.
- Nothing in this repo reads OpenTelemetry's provider or propagator globals.
- The core package compiles without the trace SDK's exporters, and gRPC is built only by apps that import it.

**Non-Goals:**

- Metrics and the OpenTelemetry logs signal. `telemetry/metric` follows in its own change and reuses decisions 2–6.
- HTTP server and client instrumentation. A later module takes the provider and propagator by injection and declares otelhttp middleware explicitly.
- A stdout exporter, `autoexport`, or `OTEL_PROPAGATORS`.
- Host, process or Kubernetes resource detectors. A collector's processors add those better.
- Mirroring every `OTEL_*` knob in Config. Timeouts, compression and batch tuning stay env-only.

## Decisions

### 1. Layout: one module, role-named packages, arrows pointing inwards

```
telemetry/                      module github.com/sikhlana/pkg/telemetry
├── config.go                   Config: endpoint, headers, tls
├── telemetry.go                resolution, resource, propagator
├── log.go                      LogMiddleware, LogMiddlewareOrder
├── module.go                   Module, InstallGlobals, With*/As* helpers
├── trace/                      package trace
│   ├── config.go               Config: sampler, sampler_arg
│   ├── trace.go                provider construction, HTTP exporter
│   ├── module.go               Module, With*/As* helpers
│   └── otlpgrpc/               package otlpgrpc: gRPC ExporterProvider
```

`trace` imports `telemetry`; `telemetry` imports neither. The two meet in the fx graph: the core provides the resource, the propagator and the resolved `Settings`, and `trace.Module` consumes them. Listing `trace.Module` without `telemetry.Module` fails startup with fx's missing-dependency error, which names what's missing.

Package `trace` shares its name with `go.opentelemetry.io/otel/trace` and `otel/sdk/trace`. Inside it they're imported as `oteltrace` and `sdktrace`, as OpenTelemetry's own code does; consumers mostly meet both only in `main`.

Identity for a role-named package: its fx.Module name and tag prefix are its path inside the repo, `telemetry/trace`, so fx logs and tags don't collide with a future `trace` elsewhere. The spec stays one per module (`telemetry`), and the commit scope is `telemetry`.

**Alternatives:** nested modules `telemetry/telemetrytrace` and `telemetrymetric` (rejected by the user: the names are worse, and the OTel SDK is a weight every user of either needs); a core that imports its signals (every consumer builds every SDK); gRPC as a nested module (cleaner module graph, but grpc's version floors are a cost only gRPC users' builds pay anyway, and a nested module would need its own name and release).

### 2. Three layers, resolved per field

> Revised in code review; see [Fixes from code review](#fixes-from-code-review).

Each field of `telemetry.Config` and `trace.Config` is resolved on its own, highest layer first:

| Layer | Source | How it reaches the SDK |
|---|---|---|
| 3. operator | Config from the graph (configloader for a service) | explicit option |
| 2. environment | the field's `OTEL_*` variables | no option; the SDK reads them |
| 1. developer | `WithDefaults` / `AsDefaults` | explicit option |

A field is set in a layer when it's non-empty: a non-empty string, a non-empty map, `tls.enabled` true. The environment layer is set when any of the field's variables is non-empty, per-signal included:

| Field | Variables |
|---|---|
| endpoint | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` |
| headers | `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_TRACES_HEADERS` |
| tls | `OTEL_EXPORTER_OTLP_CERTIFICATE`, `…_CLIENT_CERTIFICATE`, `…_CLIENT_KEY`, and their `TRACES_` forms |
| sampler (with sampler_arg) | `OTEL_TRACES_SAMPLER` |

The trick is passing no option when the environment wins: an explicit option would beat the variable, and a developer default would then override the operator's environment. So the module checks the variables only to decide which layer won, and lets the SDK parse them.

The core resolves the shared fields and provides them as `telemetry.Settings` (the winning values plus which layer each came from). It only knows the generic variables; `trace` re-checks the `TRACES_` forms for its own decision, since a per-signal variable is a trace concern.

`sampler` and `sampler_arg` resolve as one unit, by the layer that sets `sampler`. Passing `WithSampler` stops the SDK reading `OTEL_TRACES_SAMPLER_ARG`, so mixing an operator sampler with an environment arg can't work. A layer that sets only `sampler_arg` is invalid (decision 9).

Config shape mirrors the spec's names and values, so an operator reading OpenTelemetry docs recognises them: `endpoint` is a base URL like `OTEL_EXPORTER_OTLP_ENDPOINT` (the HTTP exporter appends `/v1/traces`), `headers` is a map, `sampler` takes `always_on`, `always_off`, `traceidratio`, `parentbased_always_on`, `parentbased_always_off` or `parentbased_traceidratio`, and `sampler_arg` is a string, as the variable is, so empty means unset and a ratio of 0 stays expressible.

**Alternatives:** Config plus defaults, ignoring `OTEL_*` (fights operator habit and the ecosystem's docs); `OTEL_*` plus defaults with no Config (no configloader validation, nothing in the config file); passing every resolved value as an explicit option after parsing the variables ourselves (re-implements the SDK's env parsing and drifts from it).

### 3. Off unless an endpoint is configured

Tracing is enabled when an endpoint resolves from any layer and `OTEL_SDK_DISABLED` isn't `true` (case-insensitive, per the spec). Otherwise `trace.Module` provides the API's noop `TracerProvider` and builds no exporter.

This departs from the OTLP spec, whose exporters default to `localhost:4318` (`4317` for gRPC). With that default, every CLI tool would try to export on every run. A service sets its endpoint in config or `OTEL_*` anyway, and a developer who wants the localhost default can set it with `WithDefaults`.

The noop provider still carries an incoming span context forward, so a disabled service in the middle of a call chain doesn't break propagation, and the log middleware still finds the caller's trace id.

**Alternatives:** an explicit `enabled` field (a second switch that can disagree with the endpoint); following the spec's localhost default (phones home).

### 4. Resource: wiring, then the environment on top

The core builds the resource from, in increasing precedence: the SDK's defaults (`telemetry.sdk.*` and the `unknown_service:<executable>` fallback), developer wiring (`WithServiceName`, `WithServiceVersion`, tags `telemetry:resource:service_name` and `telemetry:resource:service_version`), then `OTEL_RESOURCE_ATTRIBUTES` and `OTEL_SERVICE_NAME`. The environment on top matches decision 2: an operator renames a deployment without a rebuild.

A future kernel change could put the app's name and `cli` build-info version into the graph for `WithServiceName`/`WithServiceVersion` to read; nothing here depends on that.

### 5. The propagator is a graph value; globals are opt-in

The core provides `propagation.TextMapPropagator`, the W3C tracecontext and baggage composite, whether or not tracing is enabled. otelhttp and similar instrumentation fall back to the global propagator unless given one, so explicitly declared middleware needs it from the graph.

`telemetry.InstallGlobals` is an fx option that, when listed, sets OpenTelemetry's global propagator and, when the graph has one, global `TracerProvider`. It exists for third-party libraries that can only read globals. Without it, they trace nothing and propagate nothing; the README says so.

To keep the repo's own modules honest, `.golangci.yml` bans `otel.Tracer`, `otel.Meter`, `otel.GetTracerProvider`, `otel.GetMeterProvider` and `otel.GetTextMapPropagator` through forbidigo, with a message naming injection. The setters were banned too, later in the change: `InstallGlobals` is the one place that calls them, under a `nolint`.

**Alternatives:** always setting the globals (listing a module silently changes third-party behaviour, and tests can't run two apps); no helper at all (every app writes the same three lines).

### 6. OpenTelemetry's errors and logs reach the injected logger, always

> Revised in code review; see [Fixes from code review](#fixes-from-code-review).

`telemetry.Module` invokes a hook at construction that sets `otel.SetErrorHandler` to log at Warn through the injected `*slog.Logger`, and `otel.SetLogger` to `logr.FromSlogHandler` over the same logger's handler. Unlike decision 5's globals, these are sinks: they change where the SDK's own failures go, not what third-party code does, and the SDK has no other channel for them. Leaving them alone would send export errors to the standard library's `log` package, which the constitution keeps modules away from.

A Warn, not an Error: the collector being unreachable is expected to happen and must not look like the app failing (decision 10). Each record carries the error as a typed attribute.

The logger comes from `log.Module` as usual; when the graph has none, fx fails startup, as for every module that logs.

### 7. Exporters: HTTP built in, gRPC by wiring, the environment checked

`trace` builds the OTLP HTTP/protobuf exporter by default. `otlpgrpc.New` is a `trace.ExporterProvider`, a function from the resolved settings to a span exporter, and `otlpgrpc.ProvideExporter` registers it through `trace.WithExporter`, which records the protocol it speaks (`grpc`). The shape follows `log`'s `tint.New`/`tint.ProvideTextHandler`.

`OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`, then `OTEL_EXPORTER_OTLP_PROTOCOL`, is checked against the protocols the linked exporter speaks when tracing is enabled: `http/protobuf` and `http/json` for the HTTP exporter, `grpc` for `otlpgrpc`. The module passes no encoding option, so the HTTP exporter picks the encoding from the same variable. A mismatch fails startup with an exported sentinel. Silently exporting over a protocol the operator didn't ask for would point at the wrong port.

**Alternatives:** wiring only, ignoring the variable (an operator's setting does nothing, silently); a protocol field choosing among linked exporters (a fourth layered field for a choice the developer already made by importing `otlpgrpc`).

### 8. A supplied provider replaces the built one

`trace.WithTracerProvider(tp)` and `trace.AsTracerProvider(constructor)` supply an `oteltrace.TracerProvider` under the tag `telemetry/trace:provider:supplied`, distinct from the unnamed provider the module puts in the graph. When it's present, the module provides it unchanged, builds no exporter, and doesn't shut it down: its lifecycle is the app's, as the supplying constructor can register its own OnStop. If an endpoint also resolved, the module logs a Warn saying the export settings are unused, since a config that does nothing is usually a mistake.

### 9. Validation and startup failures

> Revised in code review; see [Fixes from code review](#fixes-from-code-review).

- `telemetry.Config.Validate`: `endpoint`, when set, is an absolute `http` or `https` URL with a host; header names are valid HTTP field names; `tls` delegates to `tlsconfig.ClientConfig`'s own validation through `configvalidator.ValidateField`.
- `trace.Config.Validate`: `sampler`, when set, is one of the spec's six names; `sampler_arg` is set only with a sampler that takes one and parses as a ratio in [0, 1]; `sampler_arg` without `sampler` is invalid.
- Operator Config is validated where it's loaded (configloader). Developer defaults never pass through configloader, so the constructors validate them and fail startup with the same errors: a developer mistake shouldn't surface as a runtime surprise.
- The protocol check (decision 7) and a TLS config that can't be built fail startup.
- Environment values other than the protocol are left to the SDK, which falls back and reports through the error handler, now our logger. Re-validating them would re-implement its parsing.

Errors follow the constitution: exported sentinels (`ErrInvalidEndpoint`, `ErrInvalidSampler`, `ErrSamplerArgWithoutSampler`, `ErrUnsupportedProtocol`, …), with detail after them.

### 10. Lifecycle: never take the app down

> Revised in code review; see [Fixes from code review](#fixes-from-code-review).

Building the provider creates the exporter, which connects lazily (HTTP) or through grpc's lazy `NewClient`, so an unreachable collector is never a startup failure. Spans buffer in the batch processor and failed exports go to the error handler.

OnStop calls the built provider's `Shutdown` with the hook's context, flushing what's buffered within kernel's stop timeout. A shutdown or flush error is logged at Warn and the hook returns nil, so a down collector doesn't turn a clean exit into a failed one. The README tells apps to keep the stop timeout above the batch processor's export timeout.

This is a deliberate exception to "a setup failure is a startup failure": telemetry is advisory.

### 11. Log correlation

`telemetry.LogMiddleware` has `log.Middleware`'s standard-library signature, so `telemetry` doesn't import `log`. For a record whose context carries a valid span context, it adds `trace_id` and `span_id` as lowercase hex strings, top-level (log's middleware contract already lands them there). Otherwise it passes the record on untouched. `LogMiddlewareOrder` (100) is the suggested order for `log.WithMiddleware`. It reads the span context through the API, so it works with a supplied provider and with the pass-through of decision 3.

**Alternatives:** a `trace` group (closer to the OpenTelemetry log data model, but backends look for top-level `trace_id`); adding `trace_flags` (the user chose not to).

### 12. Testing

- The OTLP HTTP path against an `httptest` server on 127.0.0.1:0 decoding the protobuf export request; the gRPC path against an in-process trace service on 127.0.0.1:0. Both use `go.opentelemetry.io/proto/otlp`, a test-only requirement.
- Layer precedence as a table over (operator, environment, developer) per field, with `t.Setenv`, asserting what reaches the collector (endpoint hit, headers seen, sampled or not) rather than which options were passed.
- OpenTelemetry's globals are process-wide: tests that touch them (decisions 5 and 6) don't run in parallel and restore the previous values in `t.Cleanup`.
- The SDK's `tracetest` recorder for provider-level behaviour, so no `testing/telemetrytest` module.

### Rollup into docs/telemetry.md at archive

Why OpenTelemetry instead of a request id; Config over the environment, why the environment passes no option, and why developer defaults were dropped; why no endpoint means off; why globals are opt-in but the error sink isn't; the provider seam; and why a down collector never fails startup or shutdown.

## Risks / Trade-offs

- [The per-field layer check drifts from the SDK's variable list, e.g. a new per-signal variable] → The table in decision 2 lives in one place in code, and the precedence tests name every variable.
- [Developer defaults and the environment interact surprisingly: a default endpoint plus an operator's `OTEL_EXPORTER_OTLP_HEADERS` sends env headers to the default endpoint] → That's the documented per-field rule; the README shows it, and the Warn in decision 8 doesn't apply here. Per-field beats all-or-nothing for the common case of an operator adding only headers.
- [Installing the error handler is process-wide, so two fx apps in one process (tests) log through whichever started last] → Acceptable for apps; tests restore it.
- [grpc's version floors reach every consumer's module graph through telemetry's go.mod] → Accepted in decision 1. Revisit with a nested module if it causes real conflicts.
- [`telemetry/trace` as a tag prefix and fx.Module name is a new identity rule] → Recorded in the constitution with the layout amendment.
- [A collector down at shutdown delays exit by up to the export timeout] → Bounded by kernel's stop timeout; documented.

## Migration Plan

Nothing consumes the module yet. One pull request; `tls/tlsconfig/v0.1.0` is already released, so the go.mod requires it directly. Merging publishes `telemetry/v0.1.0`. go-modules' `http/requestid` isn't ported, and its X-Request-ID echo waits for the HTTP server module.

## What actually happened

The module landed as designed in layout (decision 1), propagator and opt-in globals (5), exporter wiring and the protocol check (7), the provider seam (8) and log correlation (11). Three reviews then reshaped the rest; their sections below record each fix. Deviations found while building, before review:

- **The HTTP exporter speaks `http/json` as well as `http/protobuf`** in v1.47.0, so it registers both protocols and the protocol check accepts either. The proposal had assumed protobuf only.
- **Shutdown ordering is pinned by a dependency, not by fx's invoke order.** Decision 6 first relied on the core module's invoke running before the app's. During review the other working session replaced that with an `ErrorSink` value each signal's constructor depends on, so the sink's hooks always start before a signal's and stop after them, and the sink became a stack so apps stopping out of order hand back to each other.
- **The global setters are banned in lint too**, not only the getters, with `InstallGlobals` the one `nolint`'d caller.

Net effect on the design: decision 2's three layers became two (Config over the environment), decision 10's exporter moved to OnStart, decision 6's sink became scoped to the app's run with a standard-error fallback, and `InstallGlobals` sets the globals for good.

## Fixes from code review

The first review found ten issues. Two changed the design; the rest were code fixes.

**Developer defaults were removed (supersedes most of decision 2).** Zero meant "unset" in every layer, so an operator couldn't turn off what a default turned on: `tls.enabled: false` in operator config fell through to the developer's `true`, and an empty headers map couldn't clear default headers. A `*bool` or a mirrored TLS struct would have fixed TLS but not maps. Removing the layer fixed both and made the rest simple: with only Config and the environment, a field is either passed as an explicit option (set in Config) or not passed at all (the SDK reads the environment), so the per-field resolver, `Settings`, `Resolved`, `Layer`, `NewSettings` and the `AsDefaults`/`WithDefaults` pairs in both packages are gone. The only variables the module still reads are the endpoint ones, to decide enablement (`Config.Enabled(signal)`), and `OTEL_SDK_DISABLED`. A CLI tool supplies a Config directly when it wants one, and that Config wins over the environment like an operator's. Trace takes `telemetry.Config` from the graph itself, and `ExporterProvider` takes it instead of `Settings`. The missing-`OTEL_EXPORTER_OTLP_INSECURE` finding went away with it.

**The exporter is built on start (revises decision 10).** The provider and its batch goroutine were created in the constructor with shutdown only in OnStop, so an app that failed after construction leaked them. `trace.New` now builds an `sdktrace.TracerProvider` with no span processor, which starts nothing, and an OnStart hook builds the exporter and registers a batch processor. Spans ended before start are dropped. An exporter that can't be built (an unreadable TLS file) fails start rather than construction, matching the constitution's "prepare in OnStart".

**Code fixes:**

- TLS is used only beside an `https` endpoint in the same Config: `Config.Validate` rejects it otherwise (`ErrTLSWithoutHTTPS`), and both exporters check `Config.UseTLS`, so the gRPC exporter no longer sends TLS credentials to a plaintext endpoint (its credentials outranked the insecure flag the `http` scheme set).
- Header values are validated (`ErrInvalidHeaderValue`), naming the header, never the value.
- The endpoint's shape is checked by an `omitempty,http_url` validate tag instead of hand-rolled parsing, so `ErrInvalidEndpoint` is gone; configvalidator reports a `RuleError` for it.
- A malformed `OTEL_RESOURCE_ATTRIBUTES` (`resource.ErrPartialResource`) keeps what parsed and logs a warning instead of failing construction. `ResourceParams` gained a `Logger`.
- `trace.Config.sampler` returns an error instead of discarding the ratio's, and `New` fails on it, so an unvalidated Config can't silently sample nothing.
- The error sink is scoped to the app's run (revises decision 6). OpenTelemetry has no getter for its logger, so it can't be restored. `InstallErrorSink` now installs a handler and logger that forward to an atomically swapped current logger, and returns an uninstall function that clears it only if it's still current. `telemetry.Module` installs on start and uninstalls on stop. A stopped app's logger no longer receives records or stays reachable. The forwarding `slog.Handler` is checked with `testing/slogtest`.

## Fixes from the second code review

- **`InstallGlobals` no longer restores the globals.** The other session had made it put back what it replaced on stop. That couldn't work: OpenTelemetry's default global provider is a proxy that delegates to the first provider set, once and for good, so restoring it still forwarded to the stopped app. It also missed apps that failed before stopping, and an app stopping out of order clobbered one still running. The globals are now documented as process-wide, for apps rather than tests.
- **`OTEL_TRACES_EXPORTER` is honoured.** `none` turns traces off; `otlp` or unset changes nothing; anything else fails startup with `ErrUnsupportedExporter`. The Go SDK doesn't read the variable.
- **Endpoint variables are checked.** A set `OTEL_EXPORTER_OTLP_ENDPOINT` or `…_TRACES_ENDPOINT` that isn't an http or https URL with a host fails startup with `ErrInvalidEndpointVariable`, naming the variable and not the value; the SDK would have dropped it and exported to localhost. `Config.Enabled` now returns `(bool, error)` for both checks.
- **The supplied-provider warning is trace-specific.** It fires for a Config endpoint or sampler, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or `OTEL_TRACES_SAMPLER`, not for the shared generic endpoint variable.
- **Internal SDK errors stay at Error.** Errors that reach slog through OpenTelemetry's logr logger keep slog's Error level; the docs now say so instead of promising Warn for everything. Errors reported through the error handler (failed exports) stay at Warn.
- **One sampler switch.** `trace.Config.sampler` both validates and builds; `Validate` calls it.
- **`TestInstallGlobals`** in the core no longer hand-wires a provider with `fx.Annotate`; the provider half lives in the trace package's test with `trace.Module`.

## Fixes from the third code review

- **The lint message for the global setters** no longer claims `InstallGlobals` puts them back.
- **Environment enums are read leniently.** `OTEL_TRACES_EXPORTER` is a comma-separated list, each entry trimmed and lowercased: `none` alone turns traces off, and otherwise every entry must be `otlp` (`otlp,console` and `none,otlp` fail, naming the unsupported entries). `OTEL_EXPORTER_OTLP_PROTOCOL` and its `TRACES_` form are compared trimmed and lowercased too. The HTTP exporter itself reads the protocol variable case-sensitively, so an `HTTP/JSON` the module accepts is sent as protobuf; collectors take both, so that's left alone.
- **Only the endpoint variable that will be read is checked.** With a Config endpoint, neither variable is; otherwise `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` when it's set, else the generic one. A stale generic variable beside a valid per-signal one no longer fails startup.
- **An http Config endpoint beats TLS settings from the environment.** The SDK loads `OTEL_EXPORTER_OTLP_CERTIFICATE` and its siblings regardless of options, and the HTTP exporter then refused to start ("insecure HTTP endpoint cannot use TLS client configuration"); the gRPC exporter would have dialled TLS. `Config.Plaintext` reports an http Config endpoint, and the HTTP exporter then passes `WithTLSClientConfig(nil)`, while the gRPC exporter passes an insecure transport-credentials dial option, the only thing that outranks the credentials the environment produced. Both are covered by tests that fail without the fix.
- **With no app's logger installed, OpenTelemetry's errors and warnings go to standard error** instead of being dropped, as its own defaults would send them; informational and debug messages are still dropped. This is the one place the module writes outside an injected logger, and only when no app is running.
- **`InstallErrorSink(nil)` discards** rather than panicking on the first record, and the error handler builds its logger once per install instead of once per error (a package-level logger would trip sloglint).

## Follow-ups left open

- **`telemetry/metric`**, the second signal, in its own change, reusing the Config, `Enabled` and the sink.
- **HTTP server and client instrumentation** in a later module: otelhttp middleware declared explicitly with the injected provider and propagator, plus the one surviving piece of go-modules' `requestid`, echoing the trace id as `X-Request-ID`.
- **App identity from kernel**: kernel could put the program's name and `cli` build-info version into the graph for `WithServiceName`/`WithServiceVersion`.
- **The SDK reads the protocol variables case-sensitively**, so an `HTTP/JSON` the module accepts is exported as protobuf. Harmless with any collector; worth an upstream issue rather than a workaround.
- **`LogMiddlewareOrder` (100) is a guess** until a second middleware exists; changing it is a patch.
- **A vendor bridge** (dd-trace-go's provider, say) plugs into the provider seam with no change here; Datadog was deliberately left out of scope.
