package trace

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

func init() {
	logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return nil
		}
		return []logger.Field{
			logger.String("trace_id", sc.TraceID().String()),
			logger.String("span_id", sc.SpanID().String()),
		}
	})
}

// TraceIDKey is the trace id key name in HTTP headers.
// https://www.w3.org/TR/trace-context/#trace-id
var TraceIDKey = http.CanonicalHeaderKey("x-trace-id")

// --- gRPC metadata propagation ---

// metadataSupplier implements the propagation.TextMapCarrier interface, used to inject
// and extract the trace context in gRPC metadata.
type metadataSupplier struct {
	metadata *metadata.MD
}

func (s *metadataSupplier) Get(key string) string {
	values := s.metadata.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (s *metadataSupplier) Set(key, value string) {
	s.metadata.Set(key, value)
}

func (s *metadataSupplier) Keys() []string {
	out := make([]string, 0, len(*s.metadata))
	for key := range *s.metadata {
		out = append(out, key)
	}
	return out
}

// Inject injects the trace context into gRPC metadata.
// Used by a gRPC client to pass the current span context to the server when starting a
// request.
func Inject(ctx context.Context, metadata *metadata.MD) {
	otel.GetTextMapPropagator().Inject(ctx, &metadataSupplier{
		metadata: metadata,
	})
}

// Extract extracts the trace context from gRPC metadata.
// Used by a gRPC server to restore the span context passed by the client when receiving
// a request.
func Extract(ctx context.Context, metadata *metadata.MD) (context.Context, trace.SpanContext) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, &metadataSupplier{
		metadata: metadata,
	})
	return ctx, trace.SpanContextFromContext(ctx)
}

// --- HTTP header propagation ---

// InjectHeader injects the trace context into an HTTP header.
// Used by an HTTP client to pass the current span context to the server when starting a
// request.
func InjectHeader(ctx context.Context, header http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// ExtractHeader extracts the trace context from an HTTP header.
// Used by an HTTP server to restore the span context passed by the client when receiving
// a request.
func ExtractHeader(ctx context.Context, header http.Header) (context.Context, trace.SpanContext) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
	return ctx, trace.SpanContextFromContext(ctx)
}

// --- Helper functions ---

// ContextWithSpanContext injects a SpanContext into the context.
// Used to pass trace information across contexts, for example injecting the trace of a
// root span into an HTTP request context.
func ContextWithSpanContext(ctx context.Context, sc trace.SpanContext) context.Context {
	return trace.ContextWithSpanContext(ctx, sc)
}

// SpanContextFromContext extracts the SpanContext from the context.
func SpanContextFromContext(ctx context.Context) trace.SpanContext {
	return trace.SpanContextFromContext(ctx)
}

// TracerFromContext returns the tracer from the context.
// If the context holds a valid span, its TracerProvider is used; otherwise the global
// TracerProvider is used.
func TracerFromContext(ctx context.Context) trace.Tracer {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(TraceName)
	}
	return otel.Tracer(TraceName)
}

// TraceIDFromContext returns the trace id in the context.
// It returns an empty string when the context holds no valid span.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// SpanIDFromContext returns the span id in the context.
// It returns an empty string when the context holds no valid span.
func SpanIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasSpanID() {
		return sc.SpanID().String()
	}
	return ""
}

// StartSpan creates and starts a new span.
// It returns the context carrying the span and the span itself.
// Usage:
//
//	ctx, span := trace.StartSpan(ctx, "operation-name",
//	    trace.WithAttributes(trace.AttrString("key", "val")),
//	)
//	defer span.End()
func StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, trace.Span) {
	return TracerFromContext(ctx).Start(ctx, name, opts...)
}
