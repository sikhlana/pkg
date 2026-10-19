package telemetry

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// Config is the operator's OTLP export settings, shared by every signal. A field that's set is passed to the exporter
// and wins over the OTEL_EXPORTER_OTLP_* variables; a field that's empty leaves the exporter to read them.
type Config struct {
	// Endpoint is the collector's base URL, such as http://collector:4318. The HTTP exporter appends /v1/traces. Its
	// scheme picks plaintext (http) or TLS (https).
	Endpoint string `mapstructure:"endpoint" validate:"omitempty,http_url"`

	// Headers are sent with every export request.
	Headers map[string]string `mapstructure:"headers"`

	// TLS verifies the collector and presents a client key pair, re-reading both as they rotate. Enabling it needs an
	// https Endpoint.
	TLS tlsconfig.ClientConfig `mapstructure:"tls"`
}

// Validate returns every problem with the Config, joined: ErrInvalidHeaderName, ErrInvalidHeaderValue,
// ErrTLSWithoutHTTPS and the TLS settings' own errors, each under its config key. An empty Config is valid. The
// endpoint's shape is checked by its validate tag, which configvalidator.Validate runs.
func (c Config) Validate() error {
	var errs []error

	for _, name := range slices.Sorted(maps.Keys(c.Headers)) {
		switch {
		case !httpguts.ValidHeaderFieldName(name):
			errs = append(
				errs,
				&configvalidator.Error{Key: "headers", Err: fmt.Errorf("%w: %q", ErrInvalidHeaderName, name)},
			)
		case !httpguts.ValidHeaderFieldValue(c.Headers[name]):
			errs = append(
				errs,
				&configvalidator.Error{Key: "headers", Err: fmt.Errorf("%w: %q", ErrInvalidHeaderValue, name)},
			)
		}
	}

	if c.TLS.Enabled && !c.https() {
		errs = append(errs, &configvalidator.Error{Key: "tls", Err: ErrTLSWithoutHTTPS})
	}

	errs = append(errs, configvalidator.ValidateField(&c, &c.TLS))

	return errors.Join(errs...)
}

// Enabled reports whether signal, such as TRACES, exports. It doesn't when OTEL_SDK_DISABLED is true, or
// OTEL_<signal>_EXPORTER is none, both compared case-insensitively. Otherwise it does when Endpoint is set, or
// OTEL_EXPORTER_OTLP_ENDPOINT or the signal's own OTEL_EXPORTER_OTLP_<signal>_ENDPOINT is.
//
// OTEL_<signal>_EXPORTER is read as a comma-separated list, each entry trimmed and lowercased: none alone turns the
// signal off, and otherwise every entry must be otlp, or Enabled fails with ErrUnsupportedExporter. It fails with
// ErrInvalidEndpointVariable when the endpoint variable the exporter would read is set to something the SDK can't
// use: the signal's own when it's set, the generic one otherwise, and neither when Endpoint wins over both.
func (c Config) Enabled(signal string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return false, nil
	}

	exporterVar := "OTEL_" + signal + "_EXPORTER"
	exporters := envList(exporterVar)

	if slices.Equal(exporters, []string{"none"}) {
		return false, nil
	}

	if unsupported := slices.DeleteFunc(exporters, func(e string) bool { return e == "otlp" }); len(unsupported) > 0 {
		return false, fmt.Errorf(
			"%w: %s lists %s",
			ErrUnsupportedExporter,
			exporterVar,
			strings.Join(unsupported, ", "),
		)
	}

	if c.Endpoint != "" {
		return true, nil
	}

	name := "OTEL_EXPORTER_OTLP_" + signal + "_ENDPOINT"

	if os.Getenv(name) == "" {
		name = "OTEL_EXPORTER_OTLP_ENDPOINT"
	}

	v := os.Getenv(name)

	if v == "" {
		return false, nil
	}

	if !httpURL(v) {
		return false, fmt.Errorf("%w: %s", ErrInvalidEndpointVariable, name)
	}

	return true, nil
}

// UseTLS reports whether an exporter should use the TLS settings: they're enabled and Endpoint is an https URL. An
// exporter checks this rather than TLS.Enabled alone, so a Config that skipped Validate still never sends TLS to a
// plaintext endpoint.
func (c Config) UseTLS() bool {
	return c.TLS.Enabled && c.https()
}

// Plaintext reports whether Endpoint is set to an http URL. An exporter then turns off TLS explicitly, so TLS settings
// it would load from OTEL_EXPORTER_OTLP_CERTIFICATE and its siblings can't override the Config.
func (c Config) Plaintext() bool {
	u, err := url.Parse(c.Endpoint)

	return c.Endpoint != "" && err == nil && u.Scheme == "http"
}

// envList returns the comma-separated entries of the variable name, each trimmed and lowercased, without empty ones.
func envList(name string) []string {
	var out []string

	for entry := range strings.SplitSeq(os.Getenv(name), ",") {
		if e := strings.ToLower(strings.TrimSpace(entry)); e != "" {
			out = append(out, e)
		}
	}

	return out
}

// httpURL reports whether s is an http or https URL with a host.
func httpURL(s string) bool {
	u, err := url.Parse(s)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// https reports whether Endpoint is an https URL.
func (c Config) https() bool {
	u, err := url.Parse(c.Endpoint)

	return err == nil && u.Scheme == "https"
}
