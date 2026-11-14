package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareAllowsAnyOriginByDefault(t *testing.T) {
	called := false
	handler := Middleware(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("next handler was not called")
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q, want *", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow credentials = %q, want empty for wildcard origin", got)
	}
}

func TestMiddlewareRestrictsConfiguredOrigins(t *testing.T) {
	handler := Middleware(nil, "https://example.com")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(allowed, request)
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q, want the request origin", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q, want true", got)
	}

	rejected := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(rejected, request)
	if got := rejected.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestMiddlewareAnswersPreflightWithoutCallingNext(t *testing.T) {
	called := false
	handler := Middleware(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("preflight request reached the next handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestNotAllowedHandlerWritesSingleStatus(t *testing.T) {
	handler := NotAllowedHandler(func(w http.ResponseWriter) {
		_, _ = w.Write([]byte("denied"))
	}, "https://example.com")
	request := httptest.NewRequest(http.MethodPost, "/missing", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if recorder.Body.String() != "denied" {
		t.Fatalf("body = %q, want denied", recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestAddAllowHeaders(t *testing.T) {
	header := make(http.Header)
	AddAllowHeaders(header, "X-Token", "X-Trace")
	AddAllowHeaders(header)

	if got := header.Values("Access-Control-Allow-Headers"); len(got) != 1 || got[0] != "X-Token, X-Trace" {
		t.Fatalf("allow headers = %v", got)
	}
}
