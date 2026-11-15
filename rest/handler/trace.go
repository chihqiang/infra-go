package handler

import (
	"net/http"

	"github.com/chihqiang/infra-go/core/trace"
	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/internal/pathmatch"
	"github.com/chihqiang/infra-go/rest/internal/response"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TraceOption customizes TraceHandler.
type TraceOption func(*traceOptions)

type traceOptions struct {
	serviceName string
	ignorePaths []string
}

// WithTraceServiceName labels spans with the service that produced them.
func WithTraceServiceName(name string) TraceOption {
	return func(opts *traceOptions) {
		opts.serviceName = name
	}
}

// WithTraceIgnorePaths skips tracing for the given request paths.
func WithTraceIgnorePaths(paths ...string) TraceOption {
	return func(opts *traceOptions) {
		opts.ignorePaths = append(opts.ignorePaths, paths...)
	}
}

// TraceHandler starts a server span per request, propagates incoming trace
// context, and records the response status on the span.
func TraceHandler(opts ...TraceOption) chain.Middleware {
	var options traceOptions
	for _, opt := range opts {
		opt(&options)
	}

	ignored := pathmatch.New(options.ignorePaths)

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if ignored.Match(r.URL.Path) {
				next(w, r)
				return
			}

			spanName := spanNameFor(r)
			ctx, _ := trace.ExtractHeader(r.Context(), r.Header)
			ctx, span := otel.Tracer(trace.TraceName).Start(ctx, spanName,
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(requestAttributes(r, options.serviceName)...),
			)

			recorder := response.NewStatusWriter(w)
			completed := false
			// Finish the span from a defer so a panicking handler still ends it.
			// A panic is recorded as a 500: the outer RecoverHandler answers with
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
