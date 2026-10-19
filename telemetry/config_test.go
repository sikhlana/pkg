package telemetry_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  telemetry.Config
		want error
		key  string
	}{
		{name: "empty", cfg: telemetry.Config{}},
		{name: "valid header", cfg: telemetry.Config{Headers: map[string]string{"authorization": "Bearer x"}}},
		{
			name: "header name with a space",
			cfg:  telemetry.Config{Headers: map[string]string{"x api key": "v"}},
			want: telemetry.ErrInvalidHeaderName,
			key:  "headers",
		},
		{
			name: "empty header name",
			cfg:  telemetry.Config{Headers: map[string]string{"": "v"}},
			want: telemetry.ErrInvalidHeaderName,
			key:  "headers",
		},
		{
			name: "header value ending in a newline",
			cfg:  telemetry.Config{Headers: map[string]string{"authorization": "Bearer x\n"}},
			want: telemetry.ErrInvalidHeaderValue,
			key:  "headers",
		},
		{
			name: "tls with an https endpoint",
			cfg: telemetry.Config{
				Endpoint: "https://collector:4318",
				TLS:      tlsconfig.ClientConfig{Enabled: true},
			},
		},
		{
			name: "tls with an http endpoint",
			cfg: telemetry.Config{
				Endpoint: "http://collector:4318",
				TLS:      tlsconfig.ClientConfig{Enabled: true},
			},
			want: telemetry.ErrTLSWithoutHTTPS,
			key:  "tls",
		},
		{
			name: "tls without an endpoint",
			cfg:  telemetry.Config{TLS: tlsconfig.ClientConfig{Enabled: true}},
			want: telemetry.ErrTLSWithoutHTTPS,
			key:  "tls",
		},
		{
			name: "invalid tls",
			cfg: telemetry.Config{
				Endpoint: "https://collector:4318",
				TLS:      tlsconfig.ClientConfig{Enabled: true, CertFile: "cert.pem"},
			},
			want: tlsconfig.ErrIncompleteKeyPair,
			key:  "tls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()

			if tt.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.want)

			violation, ok := errors.AsType[*configvalidator.Error](err)
			require.True(t, ok)
			assert.Equal(t, tt.key, violation.Key)
		})
	}
}

func TestConfigValidateKeepsHeaderValuesOut(t *testing.T) {
	t.Parallel()

	err := telemetry.Config{Headers: map[string]string{"authorization": "secret-token\n"}}.Validate()

	require.ErrorIs(t, err, telemetry.ErrInvalidHeaderValue)
	assert.NotContains(t, err.Error(), "secret-token")
	assert.Contains(t, err.Error(), `"authorization"`)
}

func TestConfigEndpointTag(t *testing.T) {
	t.Parallel()

	type app struct {
		Telemetry telemetry.Config `mapstructure:"telemetry"`
	}

	tests := []struct {
		name     string
		endpoint string
		valid    bool
	}{
		{name: "empty", endpoint: "", valid: true},
		{name: "http", endpoint: "http://collector:4318", valid: true},
		{name: "https with a path", endpoint: "https://collector.example/otlp", valid: true},
		{name: "no scheme", endpoint: "collector:4318"},
		{name: "another scheme", endpoint: "grpc://collector:4317"},
		{name: "no host", endpoint: "http:///v1"},
		{name: "relative", endpoint: "/v1/traces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := configvalidator.Validate(&app{Telemetry: telemetry.Config{Endpoint: tt.endpoint}})

			if tt.valid {
				require.NoError(t, err)

				return
			}

			violation, ok := errors.AsType[*configvalidator.Error](err)
			require.True(t, ok)
			assert.Equal(t, "telemetry.endpoint", violation.Key)

			rule, ok := errors.AsType[*configvalidator.RuleError](err)
			require.True(t, ok)
			assert.Equal(t, "http_url", rule.Tag)
		})
	}
}

func TestConfigEnabled(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		env      map[string]string
		want     bool
		err      error
	}{
		{name: "nothing configured", want: false},
		{name: "an endpoint", endpoint: "http://collector:4318", want: true},
		{
			name: "the generic variable",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318"},
			want: true,
		},
		{
			name: "the signal's variable",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://c:4318/v1/traces"},
			want: true,
		},
		{
			name: "another signal's variable",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://c:4318/v1/metrics"},
			want: false,
		},
		{
			name:     "disabled",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_SDK_DISABLED": "true"},
			want:     false,
		},
		{
			name:     "disabled in capitals",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_SDK_DISABLED": "TRUE"},
			want:     false,
		},
		{
			name:     "the exporter is none",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "none"},
			want:     false,
		},
		{
			name:     "the exporter is otlp",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "otlp"},
			want:     true,
		},
		{
			name:     "none in capitals with spaces",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": " NONE "},
			want:     false,
		},
		{
			name:     "otlp in capitals",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "OTLP"},
			want:     true,
		},
		{
			name:     "a list of otlp",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "otlp, otlp"},
			want:     true,
		},
		{
			name:     "a list with another exporter",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "otlp,console"},
			err:      telemetry.ErrUnsupportedExporter,
		},
		{
			name:     "none beside otlp",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "none,otlp"},
			err:      telemetry.ErrUnsupportedExporter,
		},
		{
			name: "a malformed generic variable beside a valid signal variable",
			env: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT":        "collector :4318",
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://c:4318/v1/traces",
			},
			want: true,
		},
		{
			name:     "a malformed variable the config overrides",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector :4318"},
			want:     true,
		},
		{
			name:     "another exporter",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "console"},
			err:      telemetry.ErrUnsupportedExporter,
		},
		{
			name: "a malformed endpoint variable",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector :4318"},
			err:  telemetry.ErrInvalidEndpointVariable,
		},
		{
			name: "an endpoint variable without a scheme",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "collector:4318"},
			err:  telemetry.ErrInvalidEndpointVariable,
		},
		{
			name: "a malformed endpoint variable while the exporter is none",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector :4318", "OTEL_TRACES_EXPORTER": "none"},
			want: false,
		},
		{
			name:     "one isn't true",
			endpoint: "http://collector:4318",
			env:      map[string]string{"OTEL_SDK_DISABLED": "1"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := telemetry.Config{Endpoint: tt.endpoint}.Enabled("TRACES")

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigUnsupportedExporterNamesTheEntries(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp, Console ,zipkin")

	_, err := telemetry.Config{}.Enabled("TRACES")

	require.ErrorIs(t, err, telemetry.ErrUnsupportedExporter)
	assert.Contains(t, err.Error(), "OTEL_TRACES_EXPORTER lists console, zipkin")
}

func TestConfigPlaintext(t *testing.T) {
	t.Parallel()

	assert.True(t, telemetry.Config{Endpoint: "http://collector:4318"}.Plaintext())
	assert.False(t, telemetry.Config{Endpoint: "https://collector:4318"}.Plaintext())
	assert.False(t, telemetry.Config{}.Plaintext(), "endpoint from the environment")
}

func TestConfigUseTLS(t *testing.T) {
	t.Parallel()

	enabled := tlsconfig.ClientConfig{Enabled: true}

	assert.True(t, telemetry.Config{Endpoint: "https://collector:4318", TLS: enabled}.UseTLS())
	assert.False(t, telemetry.Config{Endpoint: "http://collector:4318", TLS: enabled}.UseTLS(), "plaintext endpoint")
	assert.False(t, telemetry.Config{TLS: enabled}.UseTLS(), "endpoint from the environment")
	assert.False(t, telemetry.Config{Endpoint: "https://collector:4318"}.UseTLS(), "tls disabled")
}
