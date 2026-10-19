package telemetry

import "errors"

var (
	// ErrInvalidHeaderName reports a header name that isn't a valid HTTP field name.
	ErrInvalidHeaderName = errors.New("invalid header name")

	// ErrInvalidHeaderValue reports a header value that isn't a valid HTTP field value, such as one ending in a
	// newline. The error names the header, never the value.
	ErrInvalidHeaderValue = errors.New("invalid header value")

	// ErrTLSWithoutHTTPS reports TLS settings enabled without an https endpoint beside them, which would never be used
	// over a plaintext endpoint.
	ErrTLSWithoutHTTPS = errors.New("tls is enabled without an https endpoint")

	// ErrUnsupportedExporter reports an OTEL_<signal>_EXPORTER naming an exporter other than otlp or none, or listing
	// none beside another. The error names the variable and the entries it doesn't support.
	ErrUnsupportedExporter = errors.New("unsupported exporter")

	// ErrInvalidEndpointVariable reports an OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_<signal>_ENDPOINT that
	// isn't an http or https URL with a host, which the SDK would drop for its localhost default. The error names the
	// variable, never its value, since a URL can carry credentials.
	ErrInvalidEndpointVariable = errors.New("invalid endpoint variable")
)
