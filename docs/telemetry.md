# Telemetry

[telemetry/README.md](../telemetry/README.md) says how to set OpenTelemetry up for an app. This page says why the module is shaped that way. The full discussion, including the alternatives rejected, is in the add-telemetry change's design.md under [openspec/changes](../openspec/changes).

## OpenTelemetry instead of a request id

go-modules carried a hand-rolled correlation id in `X-Request-ID` (`http/requestid`). It tied a log line to the request that produced it, inside one process, and nothing more. OpenTelemetry does that and the rest: trace context propagates between services over W3C `traceparent`, background work gets root spans of its own, every log record can carry the trace and span ids, and the backends already understand all of it. So `requestid` wasn't ported, and the log middleware adds `trace_id` and `span_id` instead.

## Config over the environment, and the environment passes no option

Each export setting comes from the operator's Config when it's set there, and from the `OTEL_*` variables that every OpenTelemetry document tells operators to set when it isn't. An environment that overrode a config file would surprise an operator. The variables bypass configloader, the one place in the repo where operator input doesn't come through it: renaming them to configloader's `<APP>_TELEMETRY_*` scheme would break every runbook, Helm chart and collector guide that sets `OTEL_EXPORTER_OTLP_ENDPOINT`.

The module doesn't parse those variables. A field set in Config reaches the exporter as an explicit option, which beats the variable; a field left empty reaches it as no option at all, and the exporter reads the variable itself. Parsing them in the module would mean re-implementing the SDK's parsing (headers in baggage format, millisecond timeouts, per-signal precedence) and drifting from it as the SDK changes. The only variables the module reads are the endpoint ones, to decide whether anything exports, and `OTEL_SDK_DISABLED`.

One exception runs the other way: an http endpoint in Config means plaintext even when `OTEL_EXPORTER_OTLP_CERTIFICATE` or its siblings are set. The SDK loads those whatever the options say, and then either refuses to start (HTTP) or dials TLS (gRPC), so each exporter turns TLS off explicitly for an http Config endpoint. Otherwise a CA variable set cluster-wide would break every app whose config asks for plaintext.

The variables the module does read are read leniently, as the specification asks of enum values: `OTEL_TRACES_EXPORTER` is a comma-separated list, trimmed and compared case-insensitively, and so are the protocol variables. A value the module can't honour fails startup instead of being ignored, so an operator who asks for `console` or `zipkin` finds out at once. Only the endpoint variable the exporter will actually read is checked, so a stale generic variable beside a valid per-signal one, or beside a Config endpoint, doesn't stop the app.

The sampler and its argument travel together: a Config that names a sampler passes its argument with it, because passing the SDK a sampler stops it reading `OTEL_TRACES_SAMPLER_ARG`. An argument without a sampler is rejected rather than quietly ignored.

### No developer defaults

An earlier design had a third layer below the environment: defaults a developer set in code, for CLI tools without a config file. It was dropped in review. Zero meant "unset" in every layer, so an operator couldn't switch off what a default switched on (`tls.enabled: false` fell through to the developer's `true`), and keeping the layers apart took a per-field resolver that had to track the SDK's variable list. A CLI tool supplies its Config directly instead (`fx.Supply(telemetry.Config{...})`), which then wins over the environment like any operator config; a tool that wants the environment to decide leaves the Config empty.

## No endpoint means off

OTLP's specification gives exporters a default endpoint, `localhost:4318` (4317 for gRPC). Following it would make every CLI tool built on this repo try to export on every run, to whatever happens to listen there. A service names its collector anyway, so the module exports only when the Config or the environment sets an endpoint, and otherwise hands out a noop tracer provider and builds no exporter. A tool that wants the localhost default supplies a Config naming it. `OTEL_SDK_DISABLED=true` turns export off too: the Go SDK doesn't implement it, so the module does, comparing case-insensitively as the specification says. So does `OTEL_TRACES_EXPORTER=none`, the standard way to switch one signal off while others share the endpoint; any exporter other than `otlp` fails startup rather than being ignored. An endpoint variable the SDK can't parse fails startup too, since the SDK would drop it and export to localhost.

A noop tracer still carries an incoming span context forward. A service in the middle of a call chain that doesn't export doesn't break the trace for the services around it, and its logs still carry the caller's trace id.

## Globals are opt-in, the error sink isn't

OpenTelemetry keeps a global tracer provider and propagator, and much instrumentation falls back to them. The module never sets them unless the app lists `telemetry.InstallGlobals`: listing a module shouldn't change what third-party code does, and tests that build two apps in one process shouldn't fight over them. The repo's own modules take the provider and propagator by injection, and `.golangci.yml` bans the global getters and setters so that stays true. `InstallGlobals` never puts back what it replaced. OpenTelemetry's default global provider is a proxy that delegates, once and for good, to the first provider set, so restoring it would leave it forwarding to the stopped app anyway; restoring on stop also missed apps that failed before stopping, and clobbered apps that stopped out of order. So the globals are documented as process-wide, for apps rather than tests.

The SDK's error handler and internal logger are global too, but they're sinks rather than behaviour. The batch processor and the exporters report failures through nothing else, so leaving them alone would send export errors to the standard library's `log` package, which the constitution keeps modules away from. `telemetry.Module` therefore points both at the app's logger while the app runs: errors reported through the error handler and the SDK's warnings at Warn, errors its internal logger reports at Error, its informational and debug messages below Info. OpenTelemetry can't hand back its logger to restore it, so the handler and logger it's given forward to the most recently installed logger still installed: when an app stops, records go back to the logger of an app started before it, or, with none, errors and warnings go to standard error as OpenTelemetry's own defaults would send them, rather than keeping a stopped app's logger alive. Each signal's constructor depends on the sink, so the sink is installed before any signal starts and uninstalled after every signal has stopped, and errors from the final flush are logged whatever order the app lists its modules in.

## The provider seam

The core package owns what every signal shares (the Config, the resource and the propagator), and each signal is a package that imports the core and consumes them through the fx graph, never the other way round. An app that lists only the trace signal builds no metrics SDK, and only an app that imports `otlpgrpc` builds gRPC.

An app can supply its own tracer provider instead of the built one: a vendor's bridge, a test recorder, or a provider configured beyond what the module exposes. The module then provides it unchanged and builds no exporter. Its lifecycle stays with the app, since the module didn't create it, and an endpoint that's now unused is logged, since config that does nothing is usually a mistake.

## A down collector never fails the app

Telemetry is advisory, so this is a deliberate exception to "a setup failure is a startup failure". The exporters connect lazily, so an unreachable collector doesn't fail startup; spans wait in the batch processor and failed exports are logged at Warn. The exporter is built on start, not when the provider is constructed, so an app that fails before starting leaves no exporter or batch goroutine behind; spans ended before start are dropped. On stop, the built provider is shut down within the stop hook's deadline, flushing what's buffered, and a failure is logged rather than returned, so a collector that's down doesn't turn a clean exit into a failed one. Invalid configuration is different: a bad endpoint, header, sampler or TLS setting fails at config load, and a sampler or TLS file that only shows up as broken when the app builds it fails startup, since it will never start working on its own.
