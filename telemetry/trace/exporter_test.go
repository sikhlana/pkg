package trace_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

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
