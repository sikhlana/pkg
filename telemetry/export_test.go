package telemetry

import "log/slog"

// Forwarder returns the handler behind OpenTelemetry's error handler and logger, for testing it against slog's
// handler rules.
func Forwarder() slog.Handler {
	return forwarder{}
}

// SetFallback replaces the handler records go to while no logger is installed, and returns a function that puts the
// previous one back.
func SetFallback(h slog.Handler) func() {
	previous := fallback
	fallback = h

	return func() { fallback = previous }
}

// LogrForwarder returns the handler behind OpenTelemetry's logger, for testing how it maps OpenTelemetry's levels.
func LogrForwarder() slog.Handler {
	return forwarder{fromLogr: true}
}
