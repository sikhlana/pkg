// Package telemetry sets OpenTelemetry up for an fx app: the resource that says who the app is, the propagator that
// carries trace context, and the OTLP settings its signals share, configured by an operator for a service or by a
// developer for a CLI tool. Nothing is exported until something configures an endpoint, and OpenTelemetry's own errors
// go to the app's logger. The signals live in subpackages, starting with telemetry/trace.
package telemetry
