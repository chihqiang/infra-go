package trace

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// init sets the global propagator, supporting W3C TraceContext and Baggage.
func init() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}
