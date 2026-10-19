## Purpose

Sets OpenTelemetry up for an fx app: who the app is, how trace context propagates, where spans are exported and how they're sampled, configured by an operator for a service or by a developer for a CLI tool, and silent unless something configures an endpoint.

## ADDED Requirements

### Requirement: Config wins over the environment, per field

Each export setting (the endpoint, the headers, the TLS settings, and the sampler with its argument) SHALL come from the app's config when it's set there, and otherwise from the OpenTelemetry environment variables for that setting, as OpenTelemetry defines them, a per-signal variable (such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) winning over its generic form. Each setting SHALL be decided independently of the others. The sampler's argument SHALL come with the sampler: when the config names a sampler, `OTEL_TRACES_SAMPLER_ARG` SHALL be ignored.

#### Scenario: Config wins over the environment
- **WHEN** the config sets the endpoint to collector A and `OTEL_EXPORTER_OTLP_ENDPOINT` names collector B
- **THEN** spans are exported to collector A

#### Scenario: The environment applies when the config leaves a setting unset
- **WHEN** the config sets no endpoint and `OTEL_EXPORTER_OTLP_ENDPOINT` names collector A
- **THEN** spans are exported to collector A

#### Scenario: A per-signal variable wins over the generic one
- **WHEN** the config sets no endpoint, `OTEL_EXPORTER_OTLP_ENDPOINT` names collector B and `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` names collector A
- **THEN** spans are exported to collector A

#### Scenario: Settings are decided independently
- **WHEN** the config sets the endpoint and no headers, and `OTEL_EXPORTER_OTLP_HEADERS` sets a header
- **THEN** spans are exported to the config's endpoint carrying the environment's header

#### Scenario: The sampler and its argument come together
- **WHEN** the config sets the sampler to `traceidratio` with argument `1`, and `OTEL_TRACES_SAMPLER_ARG` is `0`
- **THEN** every root span is sampled

### Requirement: Nothing is exported unless an endpoint is configured

Spans SHALL be exported only when the config or the environment sets an endpoint, `OTEL_SDK_DISABLED` isn't `true`, compared case-insensitively, and `OTEL_TRACES_EXPORTER` isn't `none`. `OTEL_TRACES_EXPORTER` SHALL be read as a comma-separated list, each entry trimmed and compared case-insensitively. With export otherwise on, an `OTEL_TRACES_EXPORTER` listing anything but `otlp` (other than `none` alone), or an endpoint variable the exporter would read that isn't an `http` or `https` URL with a host, SHALL fail startup with an error matching an exported sentinel that names the variable. The exporter reads the per-signal endpoint variable when it's set, otherwise the generic one, and neither when the config sets an endpoint; a variable it won't read SHALL NOT fail startup. Otherwise the module SHALL provide a tracer provider that records and exports nothing, and SHALL make no network connection. It SHALL NOT fall back to OTLP's default localhost endpoint.

#### Scenario: Nothing configured
- **WHEN** neither the config nor the environment sets an endpoint
- **THEN** no span is exported and no connection is attempted to localhost:4318 or localhost:4317

#### Scenario: Disabled through the environment
- **WHEN** the config sets an endpoint and `OTEL_SDK_DISABLED` is `TRUE`
- **THEN** no span is exported

#### Scenario: Any other value doesn't disable
- **WHEN** an endpoint is configured and `OTEL_SDK_DISABLED` is `1`
- **THEN** spans are exported

#### Scenario: Traces switched off by their exporter variable
- **WHEN** the config sets an endpoint and `OTEL_TRACES_EXPORTER` is `none`
- **THEN** no span is exported

#### Scenario: An unsupported exporter
- **WHEN** `OTEL_TRACES_EXPORTER` is `console`
- **THEN** startup fails with the unsupported-exporter error

#### Scenario: A malformed endpoint variable
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is `collector :4318`
- **THEN** startup fails with the invalid-endpoint-variable error, and nothing is exported to localhost

#### Scenario: A malformed variable that won't be read
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is `collector :4318` and `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` names a valid collector
- **THEN** the app starts and exports to that collector

#### Scenario: Enum values in another case
- **WHEN** an endpoint is configured and `OTEL_TRACES_EXPORTER` is ` NONE `
- **THEN** no span is exported

#### Scenario: A list naming another exporter
- **WHEN** `OTEL_TRACES_EXPORTER` is `otlp,console`
- **THEN** startup fails with the unsupported-exporter error, naming `console`

### Requirement: A disabled tracer still carries trace context forward

When nothing is exported, a span started from a context that carries a valid span context SHALL carry that same trace id and span id, so the trace context propagates to outbound calls and the logs.

#### Scenario: Pass-through when disabled
- **WHEN** no endpoint is configured and a span is started from a context carrying a remote span context with trace id T
- **THEN** the new span's context has trace id T, and injecting it with the provided propagator writes a `traceparent` with trace id T

### Requirement: The resource identifies the service, and the environment overrides wiring

Exported spans SHALL carry `service.name` and `service.version` from developer wiring when it sets them. `OTEL_SERVICE_NAME` and the attributes in `OTEL_RESOURCE_ATTRIBUTES` SHALL override the wiring, with `OTEL_SERVICE_NAME` winning over a `service.name` in `OTEL_RESOURCE_ATTRIBUTES`. With neither set, `service.name` SHALL be OpenTelemetry's `unknown_service` default.

#### Scenario: Wiring alone
- **WHEN** the developer wires the service name `notary` and version `1.2.3`, and neither variable is set
- **THEN** exported spans carry `service.name` `notary` and `service.version` `1.2.3`

#### Scenario: The environment renames the service
- **WHEN** the developer wires `notary` and `OTEL_SERVICE_NAME` is `notary-canary`
- **THEN** exported spans carry `service.name` `notary-canary`

#### Scenario: Resource attributes from the environment
- **WHEN** `OTEL_RESOURCE_ATTRIBUTES` is `deployment.environment.name=prod`
- **THEN** exported spans carry that attribute alongside the wired ones

### Requirement: The propagator is W3C tracecontext and baggage

The module SHALL provide a propagator that injects and extracts W3C `traceparent`, `tracestate` and `baggage`, whether or not anything is exported.

#### Scenario: Round trip
- **WHEN** a context with a span and a baggage member is injected into a carrier and extracted into a fresh context
- **THEN** the extracted context has the same trace id, span id and baggage member

### Requirement: Spans export over OTLP HTTP by default, or over gRPC when wired

With export enabled, spans SHALL be sent over OTLP HTTP, as protobuf unless the environment's protocol is `http/json`, to the endpoint, with `/v1/traces` appended when it came from the config. When the app wires the gRPC exporter, spans SHALL be sent over OTLP gRPC instead. An `http://` endpoint SHALL be plaintext and an `https://` one TLS.

#### Scenario: HTTP by default
- **WHEN** the endpoint is `http://127.0.0.1:<port>` and a span ends
- **THEN** an HTTP collector on that port receives a POST to `/v1/traces` containing the span

#### Scenario: gRPC when wired
- **WHEN** the app wires the gRPC exporter, the endpoint is `http://127.0.0.1:<port>` and a span ends
- **THEN** a gRPC trace service on that port receives the span

#### Scenario: Headers reach the collector
- **WHEN** the config's headers include `authorization: Bearer x`
- **THEN** the collector sees that header on the export request

### Requirement: The environment's protocol must match the wired exporter

With export enabled, when `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`, or failing that `OTEL_EXPORTER_OTLP_PROTOCOL`, is set to a protocol, compared trimmed and case-insensitively, that the wired exporter doesn't speak (the HTTP exporter speaks `http/protobuf` and `http/json`, the gRPC exporter `grpc`), startup SHALL fail with an error matching an exported sentinel that names the variable and both protocols.

#### Scenario: gRPC asked for, HTTP linked
- **WHEN** the app doesn't wire the gRPC exporter, an endpoint is configured, and `OTEL_EXPORTER_OTLP_PROTOCOL` is `grpc`
- **THEN** startup fails with the unsupported-protocol error

#### Scenario: JSON asked for over HTTP
- **WHEN** the app doesn't wire the gRPC exporter, an endpoint is configured, and `OTEL_EXPORTER_OTLP_PROTOCOL` is `http/json`
- **THEN** startup succeeds and spans are exported as JSON

#### Scenario: HTTP asked for, gRPC linked
- **WHEN** the app wires the gRPC exporter, an endpoint is configured, and `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` is `http/protobuf`
- **THEN** startup fails with the unsupported-protocol error

#### Scenario: Ignored when disabled
- **WHEN** no endpoint is configured and `OTEL_EXPORTER_OTLP_PROTOCOL` is `grpc`
- **THEN** startup succeeds

### Requirement: TLS to the collector uses rotating client settings

When the config enables TLS beside an `https` endpoint, the exporter SHALL verify the collector against the configured CA file and present the configured key pair, re-reading both as the TLS client settings module does, without a restart.

#### Scenario: A private CA
- **WHEN** TLS is enabled with a CA file that issued the collector's certificate and the endpoint is `https://127.0.0.1:<port>`
- **THEN** spans are exported to that collector

#### Scenario: An http endpoint beside TLS variables
- **WHEN** the config sets an `http://` endpoint and `OTEL_EXPORTER_OTLP_CERTIFICATE` names a CA file, over HTTP or gRPC
- **THEN** the app starts and spans are exported in plaintext

#### Scenario: TLS settings beside a plaintext endpoint
- **WHEN** a config that skipped validation enables TLS beside an `http://` endpoint, over HTTP or gRPC
- **THEN** spans are exported in plaintext

#### Scenario: An untrusted collector
- **WHEN** TLS is enabled with a CA file that didn't issue the collector's certificate
- **THEN** no span reaches the collector, the app keeps running, and the failure is logged

### Requirement: Samplers follow OpenTelemetry's names

The sampler setting SHALL accept `always_on`, `always_off`, `traceidratio`, `parentbased_always_on`, `parentbased_always_off` and `parentbased_traceidratio`, with a ratio argument for the two ratio samplers, behaving as OpenTelemetry defines them. With neither the config nor the environment setting a sampler, the sampler SHALL be `parentbased_always_on`.

#### Scenario: Always off
- **WHEN** the sampler is `always_off`
- **THEN** no span is exported

#### Scenario: Parent-based by default
- **WHEN** no sampler is set and a span starts from a remote parent that isn't sampled
- **THEN** the span isn't exported

### Requirement: Invalid configuration fails at load or startup

Validating the config, as configuration loading does, SHALL fail with an error naming the config key for: an endpoint that isn't an `http` or `https` URL with a host; a header name or value that isn't a valid HTTP field name or value, without the value appearing in the error; TLS enabled without an `https` endpoint in the same config; invalid TLS settings; a sampler outside the accepted names; a sampler argument without a sampler, with a sampler that takes none, or that isn't a ratio between 0 and 1. Every cause other than the endpoint's shape SHALL match an exported sentinel. For a config that skipped validation, a sampler the module can't build, and TLS files it can't read, SHALL fail startup.

#### Scenario: An endpoint without a scheme
- **WHEN** the config sets the endpoint to `collector:4318`
- **THEN** validating the config fails under the endpoint's key

#### Scenario: A header value ending in a newline
- **WHEN** the config sets a header whose value ends in a newline
- **THEN** validating the config fails with the invalid-header-value error, which names the header and not the value

#### Scenario: TLS beside a plaintext endpoint
- **WHEN** the config enables TLS and sets the endpoint to `http://collector:4318`
- **THEN** validating the config fails with the TLS-without-HTTPS error

#### Scenario: A ratio out of range
- **WHEN** the config sets the sampler to `traceidratio` with argument `1.5`
- **THEN** validating the config fails with the invalid-sampler-argument error, and an app given that config without validating it fails to start with the same error

#### Scenario: An argument without a sampler
- **WHEN** the config sets a sampler argument and no sampler
- **THEN** validating the config fails with the sampler-argument-without-sampler error

#### Scenario: A CA file that doesn't exist
- **WHEN** the config enables TLS with a CA file that doesn't exist, beside an `https` endpoint
- **THEN** starting the app fails

### Requirement: A malformed resource environment never fails the app

When `OTEL_RESOURCE_ATTRIBUTES` can't be fully parsed, the resource SHALL keep the attributes that did parse, a warning SHALL be logged, and the app SHALL start.

#### Scenario: An attribute without a value
- **WHEN** `OTEL_RESOURCE_ATTRIBUTES` is `deployment.environment.name=prod,service.name`
- **THEN** the app starts, the resource carries `deployment.environment.name` `prod`, and a warning is logged

### Requirement: Nothing runs in the background before start

The exporter and its background processing SHALL be created when the app starts, not when it's built. An app that fails before starting SHALL leave no exporter or background goroutine behind. Spans ended before start SHALL be dropped.

#### Scenario: A later constructor fails
- **WHEN** export is configured and another constructor in the app fails before it starts
- **THEN** no exporter is built

### Requirement: An unreachable collector never fails the app

Startup SHALL succeed when the collector is unreachable. Failed exports SHALL be logged and SHALL NOT stop the app. On stop, buffered spans SHALL be flushed within the stop hook's deadline; a flush or shutdown failure SHALL be logged and SHALL NOT make stopping fail.

#### Scenario: Nothing listening
- **WHEN** the endpoint names a port nothing listens on
- **THEN** the app starts, runs, and stops without error, and a warning about the failed export is logged

#### Scenario: Flushed on stop
- **WHEN** a span ends just before the app stops, with the collector reachable
- **THEN** the collector has received it by the time stopping returns

### Requirement: OpenTelemetry's own errors and logs go to the app's logger

Errors the OpenTelemetry SDK reports, and its internal log messages, SHALL be written through the app's injected logger from when the app starts until it stops: errors reported through its error handler and its warnings at warning level, an error carrying the error as an attribute, errors its internal logger reports at error level, informational and debug messages at or below debug level. This SHALL hold whether or not the globals are installed, and SHALL cover errors from flushing on stop, in whatever order the app lists its modules. After the app stops, they SHALL no longer reach its logger; while another app in the process is still running, they SHALL reach that app's logger, and with none running, errors and warnings SHALL go to standard error.

#### Scenario: An export failure
- **WHEN** an export fails because the collector refuses the connection
- **THEN** the app's logger receives a warning-level record carrying the error

#### Scenario: An SDK warning at the default level
- **WHEN** the app logs at Info and the SDK logs a warning
- **THEN** the app's logger writes it at warning level

#### Scenario: A flush failure on stop
- **WHEN** a module listed before the telemetry module builds the tracer provider, and the final flush on stop fails
- **THEN** the app's logger receives the error

#### Scenario: After stop
- **WHEN** the SDK reports an error after the app has stopped, with no other app running
- **THEN** the stopped app's logger receives nothing, and the error is written to standard error

#### Scenario: Another app still running
- **WHEN** two apps run in one process and the one that started last stops
- **THEN** the SDK's errors reach the other app's logger

### Requirement: The app can supply its own tracer provider

When the app supplies a tracer provider, the module SHALL provide that provider unchanged, SHALL build no exporter, and SHALL NOT shut the supplied provider down. When an endpoint or a sampler is also configured for traces, in the config or through `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or `OTEL_TRACES_SAMPLER`, the module SHALL log a warning naming the settings that are unused. `OTEL_EXPORTER_OTLP_ENDPOINT` alone SHALL NOT trigger it, since other signals share it.

#### Scenario: A supplied provider
- **WHEN** the app supplies a recording provider and an endpoint is configured
- **THEN** spans started through the module's provider are recorded by the supplied one, nothing is sent to the endpoint, a warning is logged, and the supplied provider is still usable after the app stops

#### Scenario: The shared endpoint variable alone
- **WHEN** the app supplies a provider and only `OTEL_EXPORTER_OTLP_ENDPOINT` is set
- **THEN** no warning is logged

### Requirement: Globals are installed only when asked

Listing the modules SHALL NOT change OpenTelemetry's global tracer provider or propagator. When the app opts in, the module SHALL set the global propagator to the provided one and, when a tracer provider is in the app, the global tracer provider to the provided one, and SHALL NOT put them back when the app stops: they stay process-wide.

#### Scenario: Not opted in
- **WHEN** the app lists the modules without opting in to globals
- **THEN** OpenTelemetry's global tracer provider and propagator are unchanged

#### Scenario: Opted in
- **WHEN** the app opts in to globals
- **THEN** the global tracer provider and propagator are the ones the module provides

#### Scenario: Left in place on stop
- **WHEN** an app that opted in to globals stops
- **THEN** the global tracer provider and propagator are still the ones it set

### Requirement: Log records carry the trace and span ids

The log middleware SHALL add `trace_id` and `span_id`, as lowercase hex strings, to every record logged with a context carrying a valid span context, and SHALL pass every other record on unchanged.

#### Scenario: A record inside a span
- **WHEN** a record is logged with a context carrying a span with trace id T and span id S
- **THEN** the record has `trace_id` T and `span_id` S as hex strings

#### Scenario: A record without a span
- **WHEN** a record is logged with a context that carries no span context
- **THEN** the record has neither attribute
