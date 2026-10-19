package telemetry_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/telemetry"
)

func TestModuleProvidesTheSharedValues(t *testing.T) {
	clearEnv(t)
	restoreGlobals(t)

	var (
		res        *resource.Resource
		propagator propagation.TextMapPropagator
	)

	app := fxtest.New(t,
		telemetry.Module,
		fx.Supply(slog.New(slog.DiscardHandler)),
		telemetry.WithServiceName("notary"),
		telemetry.WithServiceVersion("1.2.3"),
		fx.Populate(&res, &propagator),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, "notary", resourceAttrs(res)["service.name"])
	assert.Equal(t, "1.2.3", resourceAttrs(res)["service.version"])
	assert.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, propagator.Fields())
}

func TestModuleInstallsTheErrorSink(t *testing.T) {
	clearEnv(t)
	restoreGlobals(t)

	rec := &recorder{}

	app := fxtest.New(t, telemetry.Module, fx.Supply(slog.New(rec)))
	app.RequireStart()

	otel.Handle(errors.New("export failed"))

	app.RequireStop()

	otel.Handle(errors.New("after stop"))

	records := rec.all()
	require.Len(t, records, 1, "uninstalled on stop")
	assert.Equal(t, slog.LevelWarn, records[0].Level)
}

func TestModuleLeavesTheGlobalsAlone(t *testing.T) {
	clearEnv(t)
	restoreGlobals(t)

	propagator := propagation.TraceContext{}
	tp := noop.NewTracerProvider()

	otel.SetTextMapPropagator(propagator) //nolint:forbidigo // sets a global for the module to leave alone
	otel.SetTracerProvider(tp)            //nolint:forbidigo // sets a global for the module to leave alone

	app := fxtest.New(t,
		telemetry.Module,
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Invoke(func(propagation.TextMapPropagator) {}),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, propagator, otel.GetTextMapPropagator()) //nolint:forbidigo // checks the global wasn't changed
	assert.Equal(t, tp, otel.GetTracerProvider())            //nolint:forbidigo // checks the global wasn't changed
}

func TestInstallGlobals(t *testing.T) {
	clearEnv(t)
	restoreGlobals(t)

	var propagator propagation.TextMapPropagator

	app := fxtest.New(t,
		telemetry.Module,
		fx.Supply(slog.New(slog.DiscardHandler)),
		telemetry.InstallGlobals,
		fx.Populate(&propagator),
	)
	app.RequireStart()

	assert.Equal(t, propagator, otel.GetTextMapPropagator()) //nolint:forbidigo // checks the global was set

	app.RequireStop()

	assert.Equal(t, propagator, otel.GetTextMapPropagator(), "never put back") //nolint:forbidigo // checks it stays
}
