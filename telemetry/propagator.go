package telemetry

import (
	"go.opentelemetry.io/otel/propagation"
)

// NewPropagator returns the W3C tracecontext and baggage propagator.
func NewPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}
