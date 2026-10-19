package trace_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/telemetry/trace"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  trace.Config
		want error
		key  string
	}{
		{name: "empty"},
		{name: "always on", cfg: trace.Config{Sampler: trace.SamplerAlwaysOn}},
		{name: "always off", cfg: trace.Config{Sampler: trace.SamplerAlwaysOff}},
		{name: "parent-based always on", cfg: trace.Config{Sampler: trace.SamplerParentBasedAlwaysOn}},
		{name: "parent-based always off", cfg: trace.Config{Sampler: trace.SamplerParentBasedAlwaysOff}},
		{name: "ratio without an argument", cfg: trace.Config{Sampler: trace.SamplerTraceIDRatio}},
		{name: "ratio 0", cfg: trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "0"}},
		{
			name: "parent-based ratio",
			cfg:  trace.Config{Sampler: trace.SamplerParentBasedTraceIDRatio, SamplerArg: "0.25"},
		},
		{
			name: "unknown sampler",
			cfg:  trace.Config{Sampler: "sometimes"},
			want: trace.ErrInvalidSampler,
			key:  "sampler",
		},
		{
			name: "ratio out of range",
			cfg:  trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "1.5"},
			want: trace.ErrInvalidSamplerArg,
			key:  "sampler_arg",
		},
		{
			name: "negative ratio",
			cfg:  trace.Config{Sampler: trace.SamplerParentBasedTraceIDRatio, SamplerArg: "-0.1"},
			want: trace.ErrInvalidSamplerArg,
			key:  "sampler_arg",
		},
		{
			name: "not a number",
			cfg:  trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "half"},
			want: trace.ErrInvalidSamplerArg,
			key:  "sampler_arg",
		},
		{
			name: "NaN",
			cfg:  trace.Config{Sampler: trace.SamplerTraceIDRatio, SamplerArg: "NaN"},
			want: trace.ErrInvalidSamplerArg,
			key:  "sampler_arg",
		},
		{
			name: "an argument without a sampler",
			cfg:  trace.Config{SamplerArg: "0.5"},
			want: trace.ErrSamplerArgWithoutSampler,
			key:  "sampler_arg",
		},
		{
			name: "an argument to a sampler that takes none",
			cfg:  trace.Config{Sampler: trace.SamplerAlwaysOn, SamplerArg: "0.5"},
			want: trace.ErrSamplerArgUnused,
			key:  "sampler_arg",
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
