package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSHandlerAllowsAnyOriginByDefault(t *testing.T) {
	called := false
	handlerFn := NewCORSHandler(CORSConfig{}).Handle(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

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

func TestCORSHandlerRestrictsConfiguredOrigins(t *testing.T) {
	handlerFn := NewCORSHandler(CORSConfig{Origins: []string{"https://example.com"}}).Handle(
		func(http.ResponseWriter, *http.Request) {})

	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	handlerFn.ServeHTTP(allowed, request)
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q, want the request origin", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q, want true", got)
	}

	rejected := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://evil.example")
	handlerFn.ServeHTTP(rejected, request)
	if got := rejected.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestCORSHandlerAnswersPreflightWithoutCallingNext(t *testing.T) {
	called := false
	handlerFn := NewCORSHandler(CORSConfig{}).Handle(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if called {
		t.Fatal("preflight request reached the next handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestCORSHandlerPassesPlainOptionsToNext(t *testing.T) {
	called := false
	handlerFn := NewCORSHandler(CORSConfig{}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// An OPTIONS request without Origin/Request-Method is not a preflight and
	// must reach the route handler.
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("plain OPTIONS request did not reach the next handler")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestCORSHandlerAddsConfiguredHeadersOnPreflight(t *testing.T) {
	handlerFn := NewCORSHandler(CORSConfig{Headers: []string{"X-Token", "X-Trace"}}).Handle(
		func(http.ResponseWriter, *http.Request) {})

	preflight := httptest.NewRequest(http.MethodOptions, "/users", nil)
	preflight.Header.Set("Origin", "https://example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, preflight)

	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got !=
		allowHeaders+", X-Token, X-Trace" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestCORSHandlerMatchesOrigin(t *testing.T) {
	tests := []struct {
		allowed string
		origin  string
		want    bool
	}{
		{allowed: "https://example.com", origin: "https://example.com", want: true},
		{allowed: "https://example.com", origin: "https://api.example.com", want: false},
		{allowed: "example.com", origin: "https://example.com", want: true},
		{allowed: "example.com", origin: "https://api.example.com", want: true},
		{allowed: "example.com", origin: "http://api.example.com:8080", want: true},
		{allowed: "com", origin: "https://evil.com", want: false},
		{allowed: "example.com", origin: "https://notexample.com", want: false},
		{allowed: "example.com", origin: "ftp://api.example.com", want: false},
		{allowed: "", origin: "https://example.com", want: false},
	}
	for _, test := range tests {
		if got := NewCORSHandler(CORSConfig{}).matchesOrigin(test.allowed, test.origin); got != test.want {
			t.Errorf("matchesOrigin(%q, %q) = %v, want %v",
				test.allowed, test.origin, got, test.want)
		}
	}
}

func TestCORSHandlerKeepsPreflightHeadersOffActualResponses(t *testing.T) {
	handlerFn := NewCORSHandler(CORSConfig{Origins: []string{"https://example.com"}}).Handle(
		func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	for _, name := range []string{
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Max-Age",
	} {
		if got := recorder.Header().Get(name); got != "" {
			t.Fatalf("%s = %q, want empty on an actual response", name, got)
		}
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/users", nil)
	preflight.Header.Set("Origin", "https://example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflightRecorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(preflightRecorder, preflight)

	for _, name := range []string{
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Max-Age",
	} {
		if got := preflightRecorder.Header().Get(name); got == "" {
			t.Fatalf("%s is missing on a preflight response", name)
		}
	}
}

func TestCORSHandlerIsPreflight(t *testing.T) {
	preflight := httptest.NewRequest(http.MethodOptions, "/", nil)
	preflight.Header.Set("Origin", "https://example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	if !NewCORSHandler(CORSConfig{}).isPreflight(preflight) {
		t.Fatal("a full preflight request should be detected")
	}

	noMethod := httptest.NewRequest(http.MethodOptions, "/", nil)
	noMethod.Header.Set("Origin", "https://example.com")
	if NewCORSHandler(CORSConfig{}).isPreflight(noMethod) {
		t.Fatal("OPTIONS without a request method is not a preflight")
	}

	notOptions := httptest.NewRequest(http.MethodGet, "/", nil)
	notOptions.Header.Set("Origin", "https://example.com")
	notOptions.Header.Set("Access-Control-Request-Method", http.MethodPost)
	if NewCORSHandler(CORSConfig{}).isPreflight(notOptions) {
		t.Fatal("a non-OPTIONS request is not a preflight")
	}

	if NewCORSHandler(CORSConfig{}).isPreflight(httptest.NewRequest(http.MethodOptions, "/", nil)) {
		t.Fatal("a bare OPTIONS request is not a preflight")
	}
}
