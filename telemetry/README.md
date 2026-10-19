# telemetry

Sets OpenTelemetry up for an fx app: who the app is, how trace context propagates, and where spans are exported and how they're sampled, configured by an operator for a service or by a developer for a CLI tool. Nothing is exported until something configures an endpoint, and OpenTelemetry's own errors go to the app's logger.

## Configuration

Each setting comes from the Config when it's set there, and otherwise from the OpenTelemetry environment variables listed for it. A set field reaches the exporter as an explicit option, which beats the variables; an empty one reaches it as no option, and the SDK reads the variables itself, so their own syntax and precedence apply: a per-signal variable such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` beats its generic form, and a per-signal endpoint is used as-is, without `/v1/traces` appended.

Export is on when the Config or the environment sets an endpoint, `OTEL_SDK_DISABLED` isn't `true` (any case) and `OTEL_TRACES_EXPORTER` isn't `none` (any case). Without one, nothing is exported and nothing connects; the module doesn't fall back to OTLP's localhost default. `OTEL_TRACES_EXPORTER` is read as a comma-separated list: with export on, any entry other than `otlp` fails startup with `ErrUnsupportedExporter`. The endpoint variable the exporter would read (the `TRACES_` one when it's set, otherwise the generic one, and neither when the Config sets an endpoint) fails startup with `ErrInvalidEndpointVariable` when it isn't an http or https URL with a host, since the SDK would otherwise drop it for localhost. An http endpoint in the Config means plaintext even when `OTEL_EXPORTER_OTLP_CERTIFICATE` or its siblings are set.

configloader validates the Config when it loads it, tags included. For a Config built by hand, call `configloader.Validate` (or `configvalidator.Validate`) yourself: the modules trust what they're given, except that a sampler they can't build fails startup and TLS is never sent to a plaintext endpoint.

### Config

Shared by every signal.

| Field | Type | Variables | Description |
|-------|------|-----------|-------------|
| endpoint | string | OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_TRACES_ENDPOINT | The collector's base URL, an http or https URL with a host (the http_url validate tag), such as http://collector:4318. The HTTP exporter appends /v1/traces; the gRPC exporter uses only its host and port. The scheme picks plaintext (http) or TLS (https). |
| headers | map of string to string | OTEL_EXPORTER_OTLP_HEADERS, OTEL_EXPORTER_OTLP_TRACES_HEADERS | Sent with every export request. Names and values must be valid HTTP field names and values. |
| tls | tlsconfig.ClientConfig | OTEL_EXPORTER_OTLP_CERTIFICATE, OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE, OTEL_EXPORTER_OTLP_CLIENT_KEY, OTEL_EXPORTER_OTLP_INSECURE and their TRACES_ forms | How an https endpoint is verified, and the key pair presented to it, re-read as the files rotate. Enabling it needs an https endpoint in the same Config. See [tls/tlsconfig](../tls/tlsconfig/README.md) for its fields. |

### trace.Config

| Field | Type | Variables | Description |
|-------|------|-----------|-------------|
| sampler | string | OTEL_TRACES_SAMPLER | always_on, always_off, traceidratio, parentbased_always_on, parentbased_always_off or parentbased_traceidratio. Unset here and in the environment means parentbased_always_on. |
| sampler_arg | string | OTEL_TRACES_SAMPLER_ARG | The ratio, from 0 to 1, for traceidratio and parentbased_traceidratio; unset means 1. Passed only with sampler, so it's rejected without one or beside a sampler that takes none. |

Timeouts, compression, batching and the protocol have no Config fields; set them with their `OTEL_*` variables.

## Exported Members

### telemetry

| Name | Description |
|------|-------------|
| Module | Provides the resource, the propagator and the ErrorSink, which installs the error sink while the app runs. Needs a *slog.Logger. Leaves OpenTelemetry's globals alone. |
| InstallGlobals | An fx option that sets OpenTelemetry's global propagator to the app's and, when the app has one, its global tracer provider. Never puts them back; process-wide. |
| Config | The operator's shared export settings. |
| Config.Validate | Returns every problem, joined, each under its config key: ErrInvalidHeaderName, ErrInvalidHeaderValue, ErrTLSWithoutHTTPS and the tls settings' own errors. The endpoint is checked by its validate tag. |
| Config.Enabled | Whether a signal, such as TRACES, exports: an endpoint in the Config or the environment, OTEL_SDK_DISABLED not true and OTEL_<signal>_EXPORTER not none. Fails with ErrUnsupportedExporter or ErrInvalidEndpointVariable. |
| ErrUnsupportedExporter | An OTEL_<signal>_EXPORTER other than otlp or none. Names the variable and value. |
| ErrInvalidEndpointVariable | An endpoint variable that isn't an http or https URL with a host. Names the variable, never the value. |
| Config.UseTLS | Whether an exporter should use the tls settings: enabled, with an https endpoint. |
| Config.Plaintext | Whether the endpoint is an http URL, so an exporter turns off TLS settings the environment would load. |
| ErrInvalidHeaderName | A header name that isn't a valid HTTP field name. Carries the name. |
| ErrInvalidHeaderValue | A header value that isn't a valid HTTP field value. Carries the header's name, never the value. |
| ErrTLSWithoutHTTPS | TLS enabled without an https endpoint in the same Config. |
| ResourceParams | NewResource's inputs: a Logger, and ServiceName and ServiceVersion, both optional. |
| NewResource | Builds the resource: the SDK's attributes and unknown_service fallback, then the wired service name and version, then OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME. A malformed OTEL_RESOURCE_ATTRIBUTES is logged at Warn and what parsed is kept. |
| NewPropagator | Returns the W3C tracecontext and baggage propagator. |
| InstallErrorSink | Sends OpenTelemetry's reported errors and warnings to a logger at Warn, an error carrying the error, errors from its internal logger at Error, and its informational and debug messages below Info, until the returned function uninstalls it. With no logger installed, errors and warnings go to standard error. A nil logger discards. Process-wide: the latest logger still installed wins, and uninstalling it falls back to the one before. |
| ErrorSink | The error sink Module installs while the app runs. A signal's constructor takes it, so the signal's stop hooks run before the sink is uninstalled. |
| ErrorSinkParams | NewErrorSink's inputs: Lifecycle and Logger. |
| NewErrorSink | Appends a hook that installs Logger as the error sink on start and uninstalls it on stop. |
| LogMiddleware | A function with log.Middleware's signature that adds trace_id and span_id, as hex, to records logged inside a valid span. |
| LogMiddlewareOrder | 100, the suggested order for LogMiddleware. |
| AsServiceName | Annotates a string, or a constructor returning one, as service.name. |
| WithServiceName | Supplies service.name. |
| AsServiceVersion | Annotates a string, or a constructor returning one, as service.version. |
| WithServiceVersion | Supplies service.version. |

### telemetry/trace

| Name | Description |
|------|-------------|
| Module | Provides the app's trace.TracerProvider, built by New. Needs telemetry.Module and a *slog.Logger. |
| Params | New's inputs: Lifecycle, Logger, Resource and ErrorSink, and the optional Telemetry, Config, Exporter and Supplied. |
| New | Returns the supplied provider, a noop provider when export is off, or an SDK provider whose exporter is built on start and which is shut down on stop. Checks the environment's protocol and fails on a sampler it can't build. |
| Config | The operator's trace settings. |
| Config.Validate | Returns the problem under its config key: ErrInvalidSampler, ErrInvalidSamplerArg, ErrSamplerArgWithoutSampler or ErrSamplerArgUnused. |
| Sampler | A sampler name. A named string, so it decodes from config with no hook. |
| SamplerAlwaysOn | always_on. |
| SamplerAlwaysOff | always_off. |
| SamplerTraceIDRatio | traceidratio. |
| SamplerParentBasedAlwaysOn | parentbased_always_on, the default. |
| SamplerParentBasedAlwaysOff | parentbased_always_off. |
| SamplerParentBasedTraceIDRatio | parentbased_traceidratio. |
| ErrInvalidSampler | A sampler other than the Sampler constants. |
| ErrInvalidSamplerArg | A sampler_arg that isn't a ratio from 0 to 1. |
| ErrSamplerArgWithoutSampler | A sampler_arg without a sampler. |
| ErrSamplerArgUnused | A sampler_arg beside a sampler that takes none. |
| ErrUnsupportedProtocol | The environment's protocol isn't one the exporter speaks. Names the variable and the protocols. |
| ProtocolHTTPProtobuf | http/protobuf. |
| ProtocolHTTPJSON | http/json. |
| ProtocolGRPC | grpc. |
| ExporterProvider | Builds a span exporter from the shared Config, passing set fields as options. |
| Exporter | An ExporterProvider and the protocols its exporters speak. |
| NewHTTPExporter | The ExporterProvider for OTLP over HTTP, the default, speaking http/protobuf and http/json. |
| AsExporter | Annotates an Exporter, or a constructor returning one, as the exporter New uses. |
| WithExporter | Supplies the exporter New uses in place of OTLP over HTTP. |
| AsTracerProvider | Annotates a tracer provider, or a constructor returning one, as the provider New returns unchanged. |
| WithTracerProvider | Supplies the provider New returns unchanged. |

### telemetry/trace/otlpgrpc

| Name | Description |
|------|-------------|
| New | The trace.ExporterProvider for OTLP over gRPC. Connects lazily. |
| ProvideExporter | Wires New as trace.Module's exporter, speaking grpc. |

## Usage

### Standalone

Every constructor takes a plain struct. Build the resource, install the error sink, and hand both to `trace.New` with the Config and a lifecycle you run yourself:

```go
cfg := telemetry.Config{Endpoint: "http://localhost:4318"}

res, err := telemetry.NewResource(telemetry.ResourceParams{Logger: logger, ServiceName: "notary", ServiceVersion: version})
if err != nil {
	return err
}

uninstall := telemetry.InstallErrorSink(logger)
defer uninstall()

tp, err := trace.New(trace.Params{Lifecycle: lc, Logger: logger, Resource: res, Telemetry: cfg})
```

`lc` is an `fx.Lifecycle` you start and stop yourself, such as `fxtest.NewLifecycle(t)` in a test: starting it builds the exporter, and stopping it flushes and shuts the provider down.

`telemetry.LogMiddleware` works on its own with [log](../log/README.md): pass it to `log.WithMiddleware`, or call it from your own handler.

### With fx

List `telemetry.Module` and `trace.Module`, and give the app a `*slog.Logger` (usually `log.Module`). `trace.Module` without `telemetry.Module` fails startup with fx's missing-dependency error. Inject the provider and propagator where they're needed:

```go
fx.Invoke(func(tp oteltrace.TracerProvider, propagator propagation.TextMapPropagator) {
	tracer := tp.Tracer("github.com/acme/notary/signer")
	// ...
})
```

#### A service

Hand the Config sections over from configloader, with `fx.Out`, so the operator sets them in the config file or through configloader's env names. `OTEL_*` still works for whatever the config file leaves unset:

```go
type Config struct {
	fx.Out

	Telemetry telemetry.Config `mapstructure:"telemetry"`
	Trace     trace.Config     `mapstructure:"trace"`
}

app := fx.New(
	log.Module,
	configloader.Provide(configloader.Options[Config]{Name: "notary"}),
	telemetry.Module,
	trace.Module,
	telemetry.WithServiceName("notary"),
	telemetry.WithServiceVersion(version),
)
```

```yaml
telemetry:
  endpoint: https://collector.observability:4318
  headers:
    authorization: Bearer abc
  tls:
    enabled: true
    ca_file: /etc/otel/ca.pem
trace:
  sampler: parentbased_traceidratio
  sampler_arg: "0.1"
```

#### A CLI tool

A tool has no config file. Leave the Config out and the operator decides through `OTEL_*`: with no endpoint there, the tool never exports.

```go
app := fx.New(
	log.Module,
	telemetry.Module,
	trace.Module,
	telemetry.WithServiceName("notary-cli"),
)
```

A tool that should export somewhere specific supplies a Config. Its fields then win over `OTEL_*`, as an operator's would, and fields it leaves empty still come from the environment:

```go
fx.Supply(telemetry.Config{Endpoint: "http://localhost:4318"}),
fx.Supply(trace.Config{Sampler: trace.SamplerAlwaysOn}),
```

#### Trace ids in the logs

Register `LogMiddleware` with the log module. Records logged with a context inside a span gain `trace_id` and `span_id`; this works whether or not anything is exported, as long as the context carries a span context:

```go
log.WithMiddleware(telemetry.LogMiddlewareOrder, telemetry.LogMiddleware)
```

#### Globals

The modules never touch OpenTelemetry's global tracer provider or propagator. Instrumentation that takes a provider and propagator as options should get them from the graph. A third-party library that can only read the globals traces nothing and propagates nothing unless the app lists:

```go
telemetry.InstallGlobals
```

It sets the globals when the app is built, so invokes see them, and never puts them back: OpenTelemetry's default global provider is a proxy that delegates to the first provider set, for good, so there's nothing to restore. The globals are process-wide, so list `InstallGlobals` in an app, not in tests that build several apps in one process. The repo's lint config bans OpenTelemetry's global getters and setters everywhere else.

The error sink is different: listing `telemetry.Module` sends OpenTelemetry's errors (failed exports above all) and its internal log messages to the app's logger from start to stop, process-wide: errors reported through its error handler (failed exports above all) and its warnings at Warn, errors its internal logger reports (such as a variable it can't parse) at Error, its informational and debug messages below Info. The sink is uninstalled after every signal has stopped, so errors from the final flush are logged whatever order the modules are listed in. After stop, records go to the logger of another app still running in the process, or, with none, errors and warnings go to standard error as OpenTelemetry's defaults would send them.

#### gRPC

Spans go over OTLP HTTP unless the app wires the gRPC exporter, which only apps that import `otlpgrpc` build:

```go
otlpgrpc.ProvideExporter()
```

Point the endpoint at the collector's gRPC port (4317 by convention). When export is on, an `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` or `OTEL_EXPORTER_OTLP_PROTOCOL` that names a protocol the wired exporter doesn't speak fails startup with `ErrUnsupportedProtocol`: the HTTP exporter speaks `http/protobuf` and `http/json`, picking the encoding from that variable, and the gRPC exporter speaks `grpc`.

#### Your own provider

Supply a provider, such as a vendor's bridge or a test recorder, and `trace.Module` provides it unchanged and builds no exporter. Its lifecycle is yours: the module never shuts it down. If an endpoint or a sampler is also configured for traces (in the Config, or through `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or `OTEL_TRACES_SAMPLER`), a warning names the settings that are unused. The generic `OTEL_EXPORTER_OTLP_ENDPOINT` doesn't count, since other signals share it.

```go
trace.WithTracerProvider(tp)
trace.AsTracerProvider(func(lc fx.Lifecycle) *sdktrace.TracerProvider { /* ... */ })
```

#### Starting and stopping

A built provider gets its exporter on start, so spans ended before then are dropped, and an app that fails before starting leaves nothing running. A TLS file that can't be read fails start. The provider is shut down when the app stops, flushing buffered spans within the stop hook's deadline. A collector that's down or slow never fails the app: failed exports and a failed shutdown are logged at Warn. Keep the app's stop timeout (kernel's, or `fx.StopTimeout`) above the exporter's timeout, `OTEL_EXPORTER_OTLP_TIMEOUT`, 10 seconds by default, or the last batch can be cut off.
