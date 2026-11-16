package trace

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// customPropagator is set by StartAgent when Config has a WithPropagator option.
// It is deliberately package-local: this library never calls
// otel.SetTextMapPropagator, because OpenTelemetry only accepts the first global
// propagator and replacing it would make the application's own configuration
// impossible.
var customPropagator atomic.Pointer[propagation.TextMapPropagator]

// defaultPropagator is the W3C propagator used when neither the application nor
// WithPropagator configured one.
func defaultPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// setCustomPropagator installs the propagator selected by WithPropagator; nil
// restores automatic selection.
func setCustomPropagator(p propagation.TextMapPropagator) {
	if p == nil {
		customPropagator.Store(nil)
		return
	}
	customPropagator.Store(&p)
}

// activePropagator returns the propagator used by Inject/Extract:
//  1. the one passed through WithPropagator,
//  2. otherwise the propagator the application installed globally,
//  3. otherwise the W3C TraceContext + Baggage default.
func activePropagator() propagation.TextMapPropagator {
	if p := customPropagator.Load(); p != nil {
		return *p
	}
	if globalPropagatorConfigured() {
		return otel.GetTextMapPropagator()
	}
	return defaultPropagator()
}

// globalPropagatorConfigured reports whether the application installed a global
// propagator that actually injects trace context. OpenTelemetry wraps the global
// propagator in a delegating type whose default delegate is a no-op, so injecting a
// valid span context into an empty carrier is the reliable probe.
func globalPropagatorConfigured() bool {
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1},
		SpanID:     oteltrace.SpanID{1},
		TraceFlags: oteltrace.FlagsSampled,
	})
	carrier := propagation.MapCarrier{}
	ctx := oteltrace.ContextWithSpanContext(context.Background(), sc)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return len(carrier) > 0
}
