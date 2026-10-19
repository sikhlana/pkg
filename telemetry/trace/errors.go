package trace

import "errors"

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

// ErrUnsupportedProtocol reports an OTEL_EXPORTER_OTLP_TRACES_PROTOCOL or OTEL_EXPORTER_OTLP_PROTOCOL naming a
// protocol the app's exporter doesn't speak.
var ErrUnsupportedProtocol = errors.New("unsupported otlp protocol")
