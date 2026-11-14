package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
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
	handlerFn := TraceHandler(WithTraceServiceName("demo"))(func(w http.ResponseWriter, _ *http.Request) {
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
	handlerFn := TraceHandler(WithTraceIgnorePaths("/health"))(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	})

	if len(spans) != 0 {
		t.Fatalf("spans = %d, want none for an ignored path", len(spans))
	}
}

func TestTraceHandlerInjectsSpanIntoContext(t *testing.T) {
	var valid bool
	handlerFn := TraceHandler()(func(_ http.ResponseWriter, r *http.Request) {
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
	handlerFn := TraceHandler()(func(http.ResponseWriter, *http.Request) {})

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
	handlerFn := TraceHandler(WithTraceIgnorePaths("/health*"))(func(http.ResponseWriter, *http.Request) {})

	spans := captureSpans(t, func() {
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))
	})

	if len(spans) != 0 {
		t.Fatalf("spans = %d, want none for a wildcard-ignored path", len(spans))
	}
}
