package telemetry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/sikhlana/pkg/telemetry"
)

func TestPropagatorRoundTrip(t *testing.T) {
	t.Parallel()

	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1, 2, 3},
		SpanID:     oteltrace.SpanID{4, 5, 6},
		TraceFlags: oteltrace.FlagsSampled,
	})

	member, err := baggage.NewMember("tenant", "acme")
	require.NoError(t, err)

	bag, err := baggage.New(member)
	require.NoError(t, err)

	ctx := baggage.ContextWithBaggage(oteltrace.ContextWithSpanContext(t.Context(), sc), bag)
	carrier := propagation.MapCarrier{}
	p := telemetry.NewPropagator()

	p.Inject(ctx, carrier)

	assert.Contains(t, carrier, "traceparent")
	assert.Contains(t, carrier, "baggage")

	got := p.Extract(t.Context(), carrier)

	assert.Equal(t, sc.TraceID(), oteltrace.SpanContextFromContext(got).TraceID())
	assert.Equal(t, sc.SpanID(), oteltrace.SpanContextFromContext(got).SpanID())
	assert.Equal(t, "acme", baggage.FromContext(got).Member("tenant").Value())
}
