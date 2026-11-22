package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/ten-go/core/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func captureSpans(t *testing.T, run func()) tracetest.SpanStubs {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	run()
	return exporter.GetSpans()
}

func TestTraceHandlerRecordsStatus(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{
		Enabled: true,
		Config:  trace.Config{Name: "demo"},
	}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	})

	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "GET /users" {
		t.Fatalf("span name = %q, want GET /users", span.Name)
	}

	attrs := map[string]string{}
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["http.response.status_code"] != "500" {
		t.Fatalf("status attribute = %q, want 500", attrs["http.response.status_code"])
	}
	if attrs["service.name"] != "demo" {
		t.Fatalf("service attribute = %q, want demo", attrs["service.name"])
	}
}

func TestTraceHandlerSkipsIgnoredPaths(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true, IgnorePaths: []string{"/health"}}).Handle(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	})

	if len(spans) != 0 {
		t.Fatalf("spans = %d, want none for an ignored path", len(spans))
	}
}

func TestTraceHandlerInjectsSpanIntoContext(t *testing.T) {
	var valid bool
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(_ http.ResponseWriter, r *http.Request) {
		valid = oteltrace.SpanFromContext(r.Context()).SpanContext().IsValid()
	})

	captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	})

	if !valid {
		t.Fatal("handler context does not carry a valid span")
	}
}

func TestTraceHandlerUsesRoutePattern(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
		request = request.WithContext(ContextWithPattern(request.Context(), "GET /users/{id}"))
		handlerFn.ServeHTTP(httptest.NewRecorder(), request)
	})

	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Name != "GET /users/{id}" {
		t.Fatalf("span name = %q, want the route template", spans[0].Name)
	}
}

func TestTraceHandlerSkipsWildcardPaths(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true, IgnorePaths: []string{"/health*"}}).Handle(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))
	})

	if len(spans) != 0 {
		t.Fatalf("spans = %d, want none for a wildcard-ignored path", len(spans))
	}
}

func TestTraceHandlerEndsSpanOnPanic(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	spans := captureSpans(t, func() {
		defer func() { _ = recover() }()
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	})

	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	attrs := map[string]string{}
	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["http.response.status_code"] != "500" {
		t.Fatalf("status attribute = %q, want 500", attrs["http.response.status_code"])
	}
}

func TestTraceHandlerSetsHTTPAttributes(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		request := httptest.NewRequest(http.MethodPost, "http://api.example.com:8443/users/42?page=2", nil)
		request.Host = "api.example.com:8443"
		request.Header.Set("User-Agent", "ten-go-test/1.0")
		request.Header.Set("X-Forwarded-Proto", "https")
		request = request.WithContext(ContextWithPattern(request.Context(), "POST /users/{id}"))
		handlerFn.ServeHTTP(httptest.NewRecorder(), request)
	})

	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	attrs := map[string]string{}
	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}

	want := map[string]string{
		"http.request.method":      "POST",
		"url.path":                 "/users/42",
		"url.scheme":               "https",
		"server.address":           "api.example.com",
		"server.port":              "8443",
		"http.route":               "POST /users/{id}",
		"user_agent.original":      "ten-go-test/1.0",
		"network.protocol.name":    "http",
		"network.protocol.version": "1.1",
	}
	for key, value := range want {
		if attrs[key] != value {
			t.Errorf("attribute %s = %q, want %q", key, attrs[key], value)
		}
	}
}

func TestTraceHandlerInjectsResponseTraceContext(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	captureSpans(t, func() {
		handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users", nil))
	})

	if got := recorder.Header().Get("Traceparent"); got == "" {
		t.Fatal("response is missing the traceparent header")
	}
}

func TestTraceHandlerClientSpanStatusIsNotErrorFor4xx(t *testing.T) {
	handlerFn := NewTraceHandler(TraceConfig{Enabled: true}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	})

	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Unset {
		t.Fatalf("status = %v, want unset for a 4xx server response", spans[0].Status.Code)
	}
}
