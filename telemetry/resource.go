package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"
)

// ResourceParams holds NewResource's inputs.
type ResourceParams struct {
	fx.In

	// Logger receives a warning when OTEL_RESOURCE_ATTRIBUTES can't be fully parsed. Nil discards it.
	Logger *slog.Logger

	// ServiceName becomes service.name, unless OTEL_SERVICE_NAME or OTEL_RESOURCE_ATTRIBUTES sets it.
	ServiceName string `name:"telemetry:resource:service_name" optional:"true"`

	// ServiceVersion becomes service.version, unless OTEL_RESOURCE_ATTRIBUTES sets it.
	ServiceVersion string `name:"telemetry:resource:service_version" optional:"true"`
}

// NewResource builds the resource every signal reports: the SDK's telemetry.sdk.* attributes and its
// unknown_service fallback for service.name, then ServiceName and ServiceVersion, then OTEL_RESOURCE_ATTRIBUTES and
// OTEL_SERVICE_NAME, each winning over what came before. A malformed OTEL_RESOURCE_ATTRIBUTES is logged at Warn and
// the attributes that did parse are kept, so the environment can't stop an app from starting.
func NewResource(p ResourceParams) (*resource.Resource, error) {
	opts := []resource.Option{resource.WithTelemetrySDK(), resource.WithService()}

	if p.ServiceName != "" {
		opts = append(opts, resource.WithAttributes(semconv.ServiceName(p.ServiceName)))
	}

	if p.ServiceVersion != "" {
		opts = append(opts, resource.WithAttributes(semconv.ServiceVersion(p.ServiceVersion)))
	}

	opts = append(opts, resource.WithFromEnv())

	// None of these detectors does I/O or reads the context.
	res, err := resource.New(context.Background(), opts...)

	switch {
	case err == nil:
		return res, nil
	case errors.Is(err, resource.ErrPartialResource) && res != nil:
		logger := p.Logger

		if logger == nil {
			logger = slog.New(slog.DiscardHandler)
		}

		logger.LogAttrs(context.Background(), slog.LevelWarn, "telemetry resource is incomplete",
			slog.Any("error", err))

		return res, nil
	default:
		return nil, fmt.Errorf("building resource: %w", err)
	}
}

// AsServiceName annotates v, a string or a constructor returning one, as the resource's service.name. Wrap it in
// fx.Supply or fx.Provide.
func AsServiceName(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"telemetry:resource:service_name"`))
}

// WithServiceName supplies the resource's service.name. OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES override it.
func WithServiceName(name string) fx.Option {
	return fx.Supply(AsServiceName(name))
}

// AsServiceVersion annotates v, a string or a constructor returning one, as the resource's service.version. Wrap it
// in fx.Supply or fx.Provide.
func AsServiceVersion(v any) any {
	return fx.Annotate(v, fx.ResultTags(`name:"telemetry:resource:service_version"`))
}

// WithServiceVersion supplies the resource's service.version. OTEL_RESOURCE_ATTRIBUTES overrides it.
func WithServiceVersion(version string) fx.Option {
	return fx.Supply(AsServiceVersion(version))
}
