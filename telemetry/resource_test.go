package telemetry_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/sikhlana/pkg/telemetry"
)

func TestResource(t *testing.T) {
	tests := []struct {
		name    string
		params  telemetry.ResourceParams
		env     map[string]string
		want    map[attribute.Key]string
		missing []attribute.Key
	}{
		{
			name:    "nothing wired",
			missing: []attribute.Key{"service.version"},
		},
		{
			name:   "wiring alone",
			params: telemetry.ResourceParams{ServiceName: "notary", ServiceVersion: "1.2.3"},
			want:   map[attribute.Key]string{"service.name": "notary", "service.version": "1.2.3"},
		},
		{
			name:   "the environment renames the service",
			params: telemetry.ResourceParams{ServiceName: "notary", ServiceVersion: "1.2.3"},
			env:    map[string]string{"OTEL_SERVICE_NAME": "notary-canary"},
			want:   map[attribute.Key]string{"service.name": "notary-canary", "service.version": "1.2.3"},
		},
		{
			name:   "resource attributes from the environment",
			params: telemetry.ResourceParams{ServiceName: "notary"},
			env:    map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment.name=prod,service.version=9"},
			want: map[attribute.Key]string{
				"service.name": "notary", "service.version": "9", "deployment.environment.name": "prod",
			},
		},
		{
			name:   "OTEL_SERVICE_NAME beats the resource attributes",
			params: telemetry.ResourceParams{ServiceName: "notary"},
			env: map[string]string{
				"OTEL_SERVICE_NAME":        "from-name",
				"OTEL_RESOURCE_ATTRIBUTES": "service.name=from-attributes",
			},
			want: map[attribute.Key]string{"service.name": "from-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			res, err := telemetry.NewResource(tt.params)
			require.NoError(t, err)

			got := resourceAttrs(res)

			for k, v := range tt.want {
				assert.Equal(t, v, got[k], k)
			}

			for _, k := range tt.missing {
				assert.NotContains(t, got, k)
			}

			assert.Equal(t, "opentelemetry", got["telemetry.sdk.name"])

			if tt.params.ServiceName == "" && tt.env["OTEL_SERVICE_NAME"] == "" {
				assert.Contains(t, got["service.name"], "unknown_service")
			}
		})
	}
}

func TestResourceKeepsWhatParsed(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod,service.name")

	rec := &recorder{}

	res, err := telemetry.NewResource(telemetry.ResourceParams{Logger: slog.New(rec), ServiceName: "notary"})
	require.NoError(t, err)

	got := resourceAttrs(res)
	assert.Equal(t, "prod", got["deployment.environment.name"])
	assert.Equal(t, "notary", got["service.name"])

	records := rec.all()
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.ErrorIs(t, attrs(records[0])["error"].Any().(error), resource.ErrPartialResource)
}
