package rest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/core/ratelimit"
	"github.com/chihqiang/infra-go/rest/handler"
)

func TestWithCORSAppliesHeadersAndPreflight(t *testing.T) {
	server, err := NewServer(Config{}, WithCORS("https://example.com"), WithCORSHeaders("X-Token"))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/users", func(http.ResponseWriter, *http.Request) {})

	preflight := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	server.ServeHTTP(preflight, request)
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflight.Code, http.StatusNoContent)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := preflight.Header().Values("Access-Control-Allow-Headers"); len(got) != 2 || got[1] != "X-Token" {
		t.Fatalf("allow headers = %v, want extra X-Token value", got)
	}
}

func TestWithCORSHeadersOnNotFound(t *testing.T) {
	server, err := NewServer(Config{}, WithCORS("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("Origin", "https://example.com")
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q, want CORS headers on 404", got)
	}
}

func TestWithCustomCORSNotAllowedHandler(t *testing.T) {
	server, err := NewServer(Config{}, WithCustomCORS(
		func(http.Header) {},
		func(w http.ResponseWriter) { _, _ = w.Write([]byte("blocked")) },
		"https://example.com",
	))
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("Origin", "https://example.com")
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound || recorder.Body.String() != "blocked" {
		t.Fatalf("response = %d %q, want 404 blocked", recorder.Code, recorder.Body.String())
	}
}

func TestWithMiddlewareAppendsAfterBuiltins(t *testing.T) {
	server, err := NewServer(Config{}, WithMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Extra", "yes")
			next(w, r)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Header().Get("X-Extra") != "yes" {
		t.Fatal("option middleware did not run")
	}
}

func TestWithSignatureVerifiesBody(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"sku":"a"}`)
	server, err := NewServer(Config{}, WithSignature(handler.SignatureConfig{Keys: [][]byte{key}}))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodPost, "/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	request := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader(body))
	request.Header.Set(handler.HeaderTimestamp, "1700000000")
	request.Header.Set(handler.HeaderSignature, handler.Sign(key, 1700000000, body))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("signed request status = %d, want 201", recorder.Code)
	}

	unsigned := httptest.NewRecorder()
	server.ServeHTTP(unsigned, httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader(body)))
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned request status = %d, want 401", unsigned.Code)
	}
}

func TestWithRateLimitRejectsExcessTraffic(t *testing.T) {
	server, err := NewServer(Config{}, WithRateLimit(ratelimit.NewTokenBucket(1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/limited", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}

	second := httptest.NewRecorder()
	server.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
}

func TestCustomCorsOverridesUnmatchedResponse(t *testing.T) {
	server, err := NewServer(Config{}, WithCustomCORS(
		func(http.Header) {},
		func(w http.ResponseWriter) { _, _ = w.Write([]byte("blocked")) },
		"https://example.com",
	))
	if err != nil {
		t.Fatal(err)
	}
	server.SetNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "custom", http.StatusNotFound)
	}))

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound || recorder.Body.String() != "blocked" {
		t.Fatalf("response = %d %q, want the CORS rejection response", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestWithCORSForwardsPlainOptionsToRoute(t *testing.T) {
	server, err := NewServer(Config{}, WithCORS("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	server.HandleFunc(http.MethodOptions, "/users", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Allow", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	server.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("plain OPTIONS request did not reach the route handler")
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestWithCORSHeadersAloneDoesNotEnableCors(t *testing.T) {
	server, err := NewServer(Config{}, WithCORSHeaders("X-Token"))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/users", func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	server.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q, want empty: WithCORSHeaders must not enable CORS", got)
	}
}

func TestWithCORSHeadersBeforeWithCORSIsApplied(t *testing.T) {
	server, err := NewServer(Config{},
		WithCORSHeaders("X-Token"),
		WithCORS("https://example.com"),
	)
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/users", func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	server.ServeHTTP(recorder, request)

	values := recorder.Header().Values("Access-Control-Allow-Headers")
	found := false
	for _, value := range values {
		if strings.Contains(value, "X-Token") {
			found = true
		}
	}
	if !found {
		t.Fatalf("allow headers = %v, want X-Token", values)
	}
}
