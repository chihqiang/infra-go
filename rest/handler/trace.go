package handler

import (
	"net/http"

	"github.com/chihqiang/ten-go/core/trace"
	"github.com/chihqiang/ten-go/rest/internal/pathmatch"
	"github.com/chihqiang/ten-go/rest/internal/response"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TraceConfig configures the HTTP tracing middleware. It controls both the
// request instrumentation and, through the embedded trace.Config, the shared
// tracing agent.
type TraceConfig struct {
	Enabled     bool     `json:",optional"`
	IgnorePaths []string `json:",optional"`
	// trace.Config supplies the agent and service settings (name, endpoint,
	// batcher, sampler, ...). They are embedded, so the keys sit next to
	// enabled and ignorePaths instead of under a separate section. When
	// endpoint is set, Server.Register adds the internal trace agent service to
	// the core/service.ServiceGroup, which starts and stops it. When the
	// application already started an agent, the existing one is reused.
	trace.Config
}

// TraceHandler starts a server span per request.
type TraceHandler struct {
	config  TraceConfig
	ignored *pathmatch.Matcher
}

// NewTraceHandler creates a TraceHandler.
func NewTraceHandler(config TraceConfig) *TraceHandler {
	return &TraceHandler{
		config:  config,
		ignored: pathmatch.New(config.IgnorePaths),
	}
}

// Handle wraps next with request tracing.
func (h *TraceHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if h.ignored.Match(r.URL.Path) {
			next(w, r)
			return
		}

		spanName := spanNameFor(r)
		ctx, _ := trace.ExtractHeader(r.Context(), r.Header)
		ctx, span := otel.Tracer(trace.TraceName).Start(ctx, spanName,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(requestAttributes(r, h.config.Name)...),
		)

		recorder := response.NewStatusWriter(w)
		completed := false
		// Finish the span from a defer so a panicking handler still ends it.
		// A panic is recorded as a 500: the outer RecoveryHandler answers with
		// one, but its recovery is invisible to this middleware.
		defer func() {
			status := recorder.Status()
			if !completed {
				status = http.StatusInternalServerError
				span.SetStatus(codes.Error, "handler panic")
			} else if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			span.End()
		}()

		next(recorder, r.WithContext(ctx))
		completed = true
	}
}

// spanNameFor prefers the matched route template so spans stay low-cardinality.
func spanNameFor(r *http.Request) string {
	if pattern := PatternFromContext(r.Context()); pattern != "" {
		return pattern
	}
	return r.Method + " " + r.URL.Path
}

func requestAttributes(r *http.Request, serviceName string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("url.path", r.URL.Path),
		attribute.String("server.address", r.Host),
	}
	if serviceName != "" {
		attrs = append(attrs, attribute.String("service.name", serviceName))
	}
	return attrs
}
