package middleware

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// defaultTracerName is the tracer name, aligned with the trace package's TraceName so that
// spans created by this middleware and by trace.StartSpan share the same tracer.
const defaultTracerName = "infra-go"

// Tracing is the HTTP server-side tracing middleware.
//
// It:
//   - extracts the upstream propagated span context from the request headers (W3C traceparent)
//   - creates a server span for every request (with HTTP semantic attributes such as
//     method/path/status)
//   - injects the span into the context so downstream logger/orm/redisx modules can
//     correlate trace_id automatically
//
// It uses the global TracerProvider by default (installed via trace.StartAgent); if the
// request context already holds a valid span, that span's TracerProvider is reused
// (supporting nested tracing within one trace).
type Tracing struct {
	matcher *x.PathMatcher
	name    string
}

// NewTracing creates the HTTP server-side tracing middleware.
// ignorePaths lists the request paths that must not be traced (health checks, probes,
// metrics, and so on); requests matching a rule pass straight through without creating a
// span. Path matching is provided uniformly by the x package and supports three forms
// (see x.NewPathMatcher):
//   - exact match: e.g. "/health"
//   - prefix wildcard: ends with "*" and may cross directories, e.g. "/health*" matches
//     /health, /healthz, /health/live
//   - glob wildcard: "*" does not cross directories, e.g. "/api/*/x"
func NewTracing(ignorePaths ...string) *Tracing {
	return &Tracing{
		matcher: x.NewPathMatcher(ignorePaths),
		name:    defaultTracerName,
	}
}

// WithTracerName overrides the default tracer name (default "infra-go", matching
// trace.TraceName); an empty name keeps the default.
func (t *Tracing) WithTracerName(name string) *Tracing {
	if name != "" {
		t.name = name
	}
	return t
}

// Middleware returns the tracing middleware in the standard
// func(http.Handler) http.Handler form. It does not depend on any specific HTTP framework
// and works with plain net/http, httpx, gin, echo, and others:
//
//	// plain net/http
//	handler := middleware.NewTracing("/health*", "/metrics/*").Middleware()(mux)
//
//	// httpx (httpx.WithTracing is the built-in convenience helper, ready for server.Use)
//	server.Use(httpx.WithTracing("/health*"))
func (t *Tracing) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// paths on the ignore list pass straight through without creating a span
			if t.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// extract the upstream propagated trace context
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			spanName := r.URL.Path + " " + r.Method

			ctx, span := t.tracer(ctx).Start(ctx, spanName,
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(
					semconv.HTTPServerAttributesFromHTTPRequest("", spanName, r)...,
				),
			)
			defer span.End()

			// inject the span context; downstream modules can correlate via trace.TraceIDFromContext
			r = r.WithContext(ctx)

			// record the response status code on the span
			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			span.SetAttributes(semconv.HTTPAttributesFromHTTPStatusCode(rec.Status())...)
			span.SetStatus(semconv.SpanStatusFromHTTPStatusCodeAndSpanKind(
				rec.Status(), oteltrace.SpanKindServer))
		})
	}
}

// tracer returns the tracer to use for the current request: when the context holds a valid
// span, that span's TracerProvider is used (supporting nested tracing), otherwise the
// global TracerProvider is used.
func (t *Tracing) tracer(ctx context.Context) oteltrace.Tracer {
	if span := oteltrace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(t.name)
	}
	return otel.Tracer(t.name)
}
