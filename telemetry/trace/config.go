package trace

import "errors"

// Sampler names one of OpenTelemetry's samplers, as OTEL_TRACES_SAMPLER does. A named string, so it decodes from
// config with no hook.
type Sampler string

const (
	// SamplerAlwaysOn samples every span.
	SamplerAlwaysOn Sampler = "always_on"

	// SamplerAlwaysOff samples no span.
	SamplerAlwaysOff Sampler = "always_off"

	// SamplerTraceIDRatio samples a fraction of traces, the ratio in sampler_arg, by trace id.
	SamplerTraceIDRatio Sampler = "traceidratio"

	// SamplerParentBasedAlwaysOn follows a parent's decision, and samples every root span.
	SamplerParentBasedAlwaysOn Sampler = "parentbased_always_on"

	// SamplerParentBasedAlwaysOff follows a parent's decision, and samples no root span.
	SamplerParentBasedAlwaysOff Sampler = "parentbased_always_off"

	// SamplerParentBasedTraceIDRatio follows a parent's decision, and samples a fraction of root spans, the ratio in
	// sampler_arg, by trace id.
	SamplerParentBasedTraceIDRatio Sampler = "parentbased_traceidratio"
)

var (
	// ErrInvalidSampler reports a sampler other than the Sampler constants.
	ErrInvalidSampler = errors.New("invalid sampler")

	// ErrInvalidSamplerArg reports a sampler_arg that isn't a ratio between 0 and 1.
	ErrInvalidSamplerArg = errors.New("sampler_arg must be a ratio between 0 and 1")

	// ErrSamplerArgWithoutSampler reports a sampler_arg set without a sampler. The argument is only passed with the
	// sampler, so alone it would be ignored.
	ErrSamplerArgWithoutSampler = errors.New("sampler_arg is set without a sampler")

	// ErrSamplerArgUnused reports a sampler_arg beside a sampler that takes none.
	ErrSamplerArgUnused = errors.New("sampler_arg is only used by traceidratio and parentbased_traceidratio")
)

// Config is the operator's trace settings. A set Sampler is passed to the SDK with SamplerArg and wins over
// OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG; an empty one leaves the SDK to read both.
type Config struct {
	// Sampler picks how spans are sampled. Unset here and in the environment means parentbased_always_on.
	Sampler Sampler `mapstructure:"sampler"`

	// SamplerArg is the ratio, between 0 and 1, for traceidratio and parentbased_traceidratio. Unset means 1. A
	// string, as OTEL_TRACES_SAMPLER_ARG is, so a ratio of 0 stays expressible.
	SamplerArg string `mapstructure:"sampler_arg"`
}

// Validate returns the problem with the Config, under its config key: ErrInvalidSampler, ErrInvalidSamplerArg,
// ErrSamplerArgWithoutSampler or ErrSamplerArgUnused. An empty Config is valid.
func (c Config) Validate() error {
	_, err := c.sampler()

	return err
}
