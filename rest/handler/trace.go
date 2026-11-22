package handler

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/chihqiang/ten-go/core/trace"
	"github.com/chihqiang/ten-go/rest/internal/pathmatch"
	"github.com/chihqiang/ten-go/rest/internal/realip"
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

		spanName := h.spanName(r)
		ctx, _ := trace.ExtractHeader(r.Context(), r.Header)
		ctx, span := otel.Tracer(trace.TraceName).Start(ctx, spanName,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(h.requestAttributes(r, spanName)...),
		)
		// Echo the trace context back so clients can correlate their request
		// with the server span. This must happen before the response is written.
		trace.InjectHeader(ctx, w.Header())

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

// spanName prefers the matched route template so spans stay low-cardinality.
func (h *TraceHandler) spanName(r *http.Request) string {
	if pattern := PatternFromContext(r.Context()); pattern != "" {
		return pattern
	}
	return r.Method + " " + r.URL.Path
}

// requestAttributes builds the span attributes for r using the stable HTTP
// semantic conventions, tagging the configured service name when it is set.
func (h *TraceHandler) requestAttributes(r *http.Request, route string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("url.path", r.URL.Path),
		attribute.String("url.scheme", h.scheme(r)),
		attribute.String("network.protocol.name", "http"),
		attribute.String("network.protocol.version", h.protocolVersion(r)),
		attribute.String("server.address", h.serverAddress(r)),
	}
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		if p, err := strconv.Atoi(port); err == nil {
			attrs = append(attrs, attribute.Int("server.port", p))
		}
	}
	if route != "" {
		attrs = append(attrs, attribute.String("http.route", route))
	}
	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, attribute.String("user_agent.original", ua))
	}
	if ip := realip.FromRequest(r); ip != "" {
		attrs = append(attrs, attribute.String("client.address", ip))
	}
	if r.ContentLength > 0 {
		attrs = append(attrs, attribute.Int64("http.request.body.size", r.ContentLength))
	}
	if h.config.Name != "" {
		attrs = append(attrs, attribute.String("service.name", h.config.Name))
	}
	return attrs
}

// scheme reports the request scheme, honouring TLS and the forwarding header
// set by a reverse proxy.
func (h *TraceHandler) scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto != "" {
		return strings.ToLower(proto)
	}
	return "http"
}

// protocolVersion normalises "HTTP/1.1" to the semconv "1.1" form.
func (h *TraceHandler) protocolVersion(r *http.Request) string {
	if version := r.Proto; version != "" {
		return strings.TrimPrefix(version, "HTTP/")
	}
	return ""
}

// serverAddress returns the host part of r.Host, falling back to the raw value
// when no port is present.
func (h *TraceHandler) serverAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		return r.Host
	}
	return host
}
