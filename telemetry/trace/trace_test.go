package trace_test

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestExportsOverHTTP(t *testing.T) {
	setup(t)

	c := newCollector(t)

	run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

	requests := c.all()
	require.Len(t, requests, 1, "flushed on stop")
	assert.Equal(t, "/v1/traces", requests[0].Path)
	assert.Equal(t, "application/x-protobuf", requests[0].ContentType)
	assert.Equal(t, []string{"served"}, requests[0].Spans)
}

func TestEndpointLayers(t *testing.T) {
	tests := []struct {
		name   string
		layers func(a, b *collector) []fx.Option
	}{
		{
			name: "operator config wins over the environment",
			layers: func(a, b *collector) []fx.Option {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", b.URL)

				return []fx.Option{fx.Supply(telemetry.Config{Endpoint: a.URL})}
			},
		},
		{
			name: "the environment applies when the config leaves it unset",
			layers: func(a, _ *collector) []fx.Option {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", a.URL)

				return nil
			},
		},
		{
			name: "the per-signal variable wins over the generic one",
			layers: func(a, b *collector) []fx.Option {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", b.URL)
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", a.URL+"/v1/traces")

				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)

			want, other := newCollector(t), newCollector(t)

			run(t, &recorder{}, tt.layers(want, other)...)

			assert.Equal(t, []string{"served"}, want.spans())
			assert.Empty(t, other.all())
		})
	}
}

func TestHeaders(t *testing.T) {
	t.Run("from the config", func(t *testing.T) {
		setup(t)

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{
			Endpoint: c.URL,
			Headers:  map[string]string{"authorization": "Bearer x"},
		}))

		requests := c.all()
		require.Len(t, requests, 1)
		assert.Equal(t, "Bearer x", requests[0].Header.Get("Authorization"))
	})

	t.Run("from the environment, beside the config's endpoint", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer y")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

		requests := c.all()
		require.Len(t, requests, 1)
		assert.Equal(t, "Bearer y", requests[0].Header.Get("Authorization"))
	})
}

func TestTLS(t *testing.T) {
	t.Run("a private CA", func(t *testing.T) {
		setup(t)

		trusted := newPKI(t)
		c := newTLSCollector(t, trusted.Leaf)

		run(t, &recorder{}, fx.Supply(telemetry.Config{
			Endpoint: c.URL,
			TLS:      tlsconfig.ClientConfig{Enabled: true, CAFile: trusted.CAFile, ServerName: "127.0.0.1"},
		}))

		assert.Equal(t, []string{"served"}, c.spans())
	})

	t.Run("an untrusted collector", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "500")

		c := newTLSCollector(t, newPKI(t).Leaf)
		rec := &recorder{}

		run(t, rec, fx.Supply(telemetry.Config{
			Endpoint: c.URL,
			TLS:      tlsconfig.ClientConfig{Enabled: true, CAFile: newPKI(t).CAFile, ServerName: "127.0.0.1"},
		}))

		assert.Empty(t, c.all())
		assert.NotEmpty(t, rec.warnings(), "the failure is logged")
	})
}

func TestSamplers(t *testing.T) {
	tests := []struct {
		name   string
		cfg    trace.Config
		parent *oteltrace.SpanContext
		want   bool
	}{
		{name: "always on", cfg: trace.Config{Sampler: trace.SamplerAlwaysOn}, want: true},
		{name: "always off", cfg: trace.Config{Sampler: trace.SamplerAlwaysOff}, want: false},
		{name: "ratio 1", cfg: trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "1"}, want: true},
		{name: "ratio 0", cfg: trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "0"}, want: false},
		{name: "ratio unset", cfg: trace.Config{Sampler: trace.SamplerTraceIDRatio}, want: true},
		{name: "parent-based always on", cfg: trace.Config{Sampler: trace.SamplerParentBasedAlwaysOn}, want: true},
		{name: "parent-based always off", cfg: trace.Config{Sampler: trace.SamplerParentBasedAlwaysOff}, want: false},
		{
			name: "parent-based ratio 0",
			cfg:  trace.Config{Sampler: trace.SamplerParentBasedTraceIDRatio, SamplerArg: "0"},
			want: false,
		},
		{
			name:   "parent-based ratio 0 under a sampled parent",
			cfg:    trace.Config{Sampler: trace.SamplerParentBasedTraceIDRatio, SamplerArg: "0"},
			parent: remoteParent(oteltrace.FlagsSampled),
			want:   true,
		},
		{name: "parent-based by default, under an unsampled parent", parent: remoteParent(0), want: false},
		{name: "parent-based by default, a root", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)

			c := newCollector(t)

			var tp oteltrace.TracerProvider

			app := fxtest.New(t,
				telemetry.Module,
				trace.Module,
				fx.Supply(slog.New(slog.DiscardHandler)),
				fx.Supply(telemetry.Config{Endpoint: c.URL}),
				fx.Supply(tt.cfg),
				fx.Populate(&tp),
			)
			app.RequireStart()

			ctx := t.Context()

			if tt.parent != nil {
				ctx = oteltrace.ContextWithRemoteSpanContext(ctx, *tt.parent)
			}

			_, span := tp.Tracer("test").Start(ctx, "served")
			span.End()

			app.RequireStop()

			if tt.want {
				assert.Equal(t, []string{"served"}, c.spans())
			} else {
				assert.Empty(t, c.spans())
			}
		})
	}
}

func TestSamplerLayers(t *testing.T) {
	t.Run("the sampler and its argument come from the same layer", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}),
			fx.Supply(trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "1"}),
		)

		assert.Equal(t, []string{"served"}, c.spans())
	})

	t.Run("the environment applies when the config names no sampler", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_TRACES_SAMPLER", "always_off")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

		assert.Empty(t, c.spans())
	})

	t.Run("the config wins over the environment", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_TRACES_SAMPLER", "always_off")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}),
			fx.Supply(trace.Config{Sampler: trace.SamplerAlwaysOn}),
		)

		assert.Equal(t, []string{"served"}, c.spans())
	})
}

func TestPlaintextConfigBeatsEnvironmentTLS(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", newPKI(t).CAFile)

	c := newCollector(t)

	run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

	assert.Equal(t, []string{"served"}, c.spans())
}

func TestProtocolIgnoresCaseAndSpace(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", " HTTP/Protobuf ")

	c := newCollector(t)

	run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

	assert.Equal(t, []string{"served"}, c.spans())
}

func TestProtocol(t *testing.T) {
	t.Run("gRPC asked for, HTTP linked", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

		err := startErr(fx.Supply(telemetry.Config{Endpoint: newCollector(t).URL}))

		require.ErrorIs(t, err, trace.ErrUnsupportedProtocol)
		assert.Contains(
			t,
			err.Error(),
			`OTEL_EXPORTER_OTLP_PROTOCOL is "grpc", and the exporter speaks http/protobuf, http/json`,
		)
	})

	t.Run("the per-signal variable wins", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")

		err := startErr(fx.Supply(telemetry.Config{Endpoint: newCollector(t).URL}))

		require.ErrorIs(t, err, trace.ErrUnsupportedProtocol)
		assert.Contains(t, err.Error(), "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")
	})

	t.Run("JSON asked for over HTTP", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/json")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

		requests := c.all()
		require.Len(t, requests, 1)
		assert.Equal(t, "application/json", requests[0].ContentType)
		assert.Equal(t, []string{"served"}, requests[0].Spans)
	})

	t.Run("ignored when disabled", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

		require.NoError(t, startErr())
	})
}

func TestDisabled(t *testing.T) {
	t.Run("nothing configured passes trace context through", func(t *testing.T) {
		setup(t)

		var (
			tp         oteltrace.TracerProvider
			propagator propagation.TextMapPropagator
		)

		app := fxtest.New(t,
			telemetry.Module,
			trace.Module,
			fx.Supply(slog.New(slog.DiscardHandler)),
			fx.Populate(&tp, &propagator),
		)
		app.RequireStart()

		assert.IsType(t, noop.TracerProvider{}, tp, "no exporter is built")

		parent := remoteParent(oteltrace.FlagsSampled)
		ctx, span := tp.Tracer("test").Start(oteltrace.ContextWithRemoteSpanContext(t.Context(), *parent), "served")
		span.End()

		assert.Equal(t, parent.TraceID(), span.SpanContext().TraceID())
		assert.Equal(t, parent.SpanID(), span.SpanContext().SpanID())

		carrier := propagation.MapCarrier{}
		propagator.Inject(ctx, carrier)

		assert.Contains(t, carrier.Get("traceparent"), parent.TraceID().String())

		app.RequireStop()
	})

	t.Run("disabled through the environment", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_SDK_DISABLED", "TRUE")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

		assert.Empty(t, c.all())
	})

	t.Run("any other value doesn't disable", func(t *testing.T) {
		setup(t)
		t.Setenv("OTEL_SDK_DISABLED", "1")

		c := newCollector(t)

		run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

		assert.Equal(t, []string{"served"}, c.spans())
	})
}

func TestSuppliedProvider(t *testing.T) {
	setup(t)

	c := newCollector(t)
	spans := tracetest.NewSpanRecorder()
	supplied := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	rec := &recorder{}

	run(t, rec, fx.Supply(telemetry.Config{Endpoint: c.URL}), trace.WithTracerProvider(supplied))

	_, span := supplied.Tracer("test").Start(t.Context(), "after stop")
	span.End()

	var names []string

	for _, s := range spans.Ended() {
		names = append(names, s.Name())
	}

	assert.Equal(t, []string{"served", "after stop"}, names, "still usable after the app stops")
	assert.Empty(t, c.all())
	require.Len(t, rec.warnings(), 1)
	assert.Contains(t, rec.warnings()[0].Message, "settings are unused")
	assert.Equal(t, "endpoint", attrValue(rec.warnings()[0], "unused"))
}

func TestSuppliedProviderUnusedSettings(t *testing.T) {
	tests := []struct {
		name string
		opts []fx.Option
		env  map[string]string
		want string
	}{
		{
			name: "a sampler",
			opts: []fx.Option{fx.Supply(trace.Config{Sampler: trace.SamplerAlwaysOff})},
			want: "sampler",
		},
		{
			name: "an endpoint and a sampler",
			opts: []fx.Option{
				fx.Supply(telemetry.Config{Endpoint: "http://127.0.0.1:4318"}),
				fx.Supply(trace.Config{Sampler: trace.SamplerAlwaysOff}),
			},
			want: "endpoint, sampler",
		},
		{
			name: "the trace endpoint and sampler variables",
			env: map[string]string{
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://c:4318/v1/traces",
				"OTEL_TRACES_SAMPLER":                "always_off",
			},
			want: "endpoint, sampler",
		},
		{
			name: "the generic endpoint variable doesn't count",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318"},
			want: "",
		},
		{name: "nothing", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			rec := &recorder{}

			run(t, rec, append(tt.opts, trace.WithTracerProvider(noop.NewTracerProvider()))...)

			if tt.want == "" {
				assert.Empty(t, rec.warnings())

				return
			}

			require.Len(t, rec.warnings(), 1)
			assert.Equal(t, tt.want, attrValue(rec.warnings()[0], "unused"))
		})
	}
}

func TestNothingListening(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "500")

	rec := &recorder{}

	run(t, rec, fx.Supply(telemetry.Config{Endpoint: "http://" + closedPort(t)}))

	warnings := rec.warnings()
	require.NotEmpty(t, warnings)
	assert.True(t, hasError(warnings[0]), "the warning carries the error")
}

func TestFlushErrorsReachTheLoggerWhateverTheOrder(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "500")

	rec := &recorder{}

	var tp oteltrace.TracerProvider

	app := fxtest.New(t,
		// Listed first, so its invoke builds the provider before telemetry.Module's invoke runs.
		fx.Module("early", fx.Invoke(func(p oteltrace.TracerProvider) { tp = p })),
		telemetry.Module,
		trace.Module,
		fx.Supply(slog.New(rec)),
		fx.Supply(telemetry.Config{Endpoint: "http://" + closedPort(t)}),
	)
	app.RequireStart()

	_, span := tp.Tracer("test").Start(t.Context(), "served")
	span.End()

	app.RequireStop()

	var sunk int

	for _, r := range rec.warnings() {
		if r.Message == "opentelemetry error" {
			sunk++
		}
	}

	assert.Positive(t, sunk, "the failed flush went through the error sink before it was uninstalled")
}

func TestUnvalidatedSamplerFailsStartup(t *testing.T) {
	setup(t)

	err := startErr(
		fx.Supply(telemetry.Config{Endpoint: newCollector(t).URL}),
		fx.Supply(trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "1.5"}),
	)

	require.ErrorIs(t, err, trace.ErrInvalidSamplerArg)
	assert.Contains(t, err.Error(), "building sampler: sampler_arg: ")
}

func TestExporterBuiltOnStart(t *testing.T) {
	t.Run("not built when the app never starts", func(t *testing.T) {
		setup(t)

		var built atomic.Int32

		errLater := errors.New("a later constructor failed")

		app := fx.New(
			fx.NopLogger,
			telemetry.Module,
			trace.Module,
			fx.Supply(slog.New(slog.DiscardHandler)),
			fx.Supply(telemetry.Config{Endpoint: newCollector(t).URL}),
			trace.WithExporter(countingExporter(&built)),
			fx.Invoke(func(oteltrace.TracerProvider) error { return errLater }),
		)

		require.ErrorIs(t, app.Err(), errLater)
		assert.Zero(t, built.Load())
	})

	t.Run("a failure to build it fails start", func(t *testing.T) {
		setup(t)

		var tp oteltrace.TracerProvider

		app := fx.New(
			fx.NopLogger,
			telemetry.Module,
			trace.Module,
			fx.Supply(slog.New(slog.DiscardHandler)),
			fx.Supply(telemetry.Config{
				Endpoint: "https://127.0.0.1:4318",
				TLS:      tlsconfig.ClientConfig{Enabled: true, CAFile: filepath.Join(t.TempDir(), "missing.pem")},
			}),
			fx.Populate(&tp),
		)
		require.NoError(t, app.Err())

		err := app.Start(t.Context())

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "building span exporter: ")
	})
}

func TestTLSOnlyForHTTPS(t *testing.T) {
	setup(t)

	c := newCollector(t)

	// An unvalidated Config pairing TLS with a plaintext endpoint still exports in plaintext.
	run(t, &recorder{}, fx.Supply(telemetry.Config{
		Endpoint: c.URL,
		TLS:      tlsconfig.ClientConfig{Enabled: true, CAFile: newPKI(t).CAFile},
	}))

	assert.Equal(t, []string{"served"}, c.spans())
}

// countingExporter returns an HTTP exporter that counts how often it's built.
func countingExporter(built *atomic.Int32) trace.Exporter {
	return trace.Exporter{
		Provider: func(ctx context.Context, cfg telemetry.Config, logger *slog.Logger) (sdktrace.SpanExporter, error) {
			built.Add(1)

			return trace.NewHTTPExporter(ctx, cfg, logger)
		},
		Protocols: []string{trace.ProtocolHTTPProtobuf},
	}
}

func TestInstallGlobals(t *testing.T) {
	setup(t)

	var (
		tp         oteltrace.TracerProvider
		propagator propagation.TextMapPropagator
	)

	app := fxtest.New(t,
		telemetry.Module,
		trace.Module,
		telemetry.InstallGlobals,
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Populate(&tp, &propagator),
	)
	app.RequireStart()

	assert.Equal(t, tp, otel.GetTracerProvider())            //nolint:forbidigo // checks the global was set
	assert.Equal(t, propagator, otel.GetTextMapPropagator()) //nolint:forbidigo // checks the global was set

	app.RequireStop()

	assert.Equal(t, tp, otel.GetTracerProvider(), "never put back") //nolint:forbidigo // checks the global stays
}

func TestEnvironmentFailsStartup(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{
			name: "an exporter other than otlp",
			env:  map[string]string{"OTEL_TRACES_EXPORTER": "console"},
			want: telemetry.ErrUnsupportedExporter,
		},
		{
			name: "a malformed endpoint variable",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector :4318"},
			want: telemetry.ErrInvalidEndpointVariable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			require.ErrorIs(t, startErr(), tt.want)
		})
	}
}

func TestExporterNone(t *testing.T) {
	setup(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "none")

	c := newCollector(t)

	run(t, &recorder{}, fx.Supply(telemetry.Config{Endpoint: c.URL}))

	assert.Empty(t, c.all())
}

// remoteParent returns a remote span context with flags.
func remoteParent(flags oteltrace.TraceFlags) *oteltrace.SpanContext {
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{0xab, 1},
		SpanID:     oteltrace.SpanID{0xcd, 2},
		TraceFlags: flags,
		Remote:     true,
	})

	return &sc
}
