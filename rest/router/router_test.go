package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_MethodPatternAndPathValue(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.PathValue("id")))
	}))

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	if got := r.MatchedPattern(req); got != "GET /users/{id}" {
		t.Fatalf("matched pattern = %q", got)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "42" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouter_NotFoundDistinguishesMethodMismatch(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/users", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if !r.IsNotFound(httptest.NewRequest(http.MethodGet, "/missing", nil)) {
		t.Fatal("unmatched path should be a 404")
	}
	if r.IsNotFound(httptest.NewRequest(http.MethodPost, "/users", nil)) {
		t.Fatal("method mismatch should remain a 405, not be treated as a 404")
	}
	if !r.IsMethodNotAllowed(httptest.NewRequest(http.MethodPost, "/users", nil)) {
		t.Fatal("method mismatch should be reported as a 405")
	}
	if r.IsMethodNotAllowed(httptest.NewRequest(http.MethodGet, "/missing", nil)) {
		t.Fatal("unmatched path should not be reported as a 405")
	}
}

func TestRouter_CustomNotFoundPreservesMethodNotAllowed(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/users", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.SetNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "custom not found", http.StatusNotFound)
	}))
	r.SetNotAllowedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "custom method not allowed", http.StatusMethodNotAllowed)
	}))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if notFound.Code != http.StatusNotFound || notFound.Body.String() != "custom not found\n" {
		t.Fatalf("not found response = %d %q", notFound.Code, notFound.Body.String())
	}

	methodMismatch := httptest.NewRecorder()
	r.ServeHTTP(methodMismatch, httptest.NewRequest(http.MethodPost, "/users", nil))
	if methodMismatch.Code != http.StatusMethodNotAllowed || methodMismatch.Body.String() != "custom method not allowed\n" {
		t.Fatalf("method mismatch response = %d %q", methodMismatch.Code, methodMismatch.Body.String())
	}
}

func TestPatternAndJoinPath(t *testing.T) {
	if got := Pattern("get", "users"); got != "GET /users" {
		t.Fatalf("Pattern() = %q", got)
	}
	if got := JoinPath("/api/", "/files/"); got != "/api/files/" {
		t.Fatalf("JoinPath() = %q", got)
	}
	if got := JoinPath("", "files"); got != "/files" {
		t.Fatalf("JoinPath() with empty prefix = %q", got)
	}
	if got := JoinPath("", ""); got != "/" {
		t.Fatalf("JoinPath() with empty paths = %q", got)
	}
}

func TestRouterMuxExposesServeMux(t *testing.T) {
	r := New()
	r.Mux().Handle("GET /direct", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/direct", nil))

	if recorder.Body.String() != "direct" {
		t.Fatalf("body = %q, want direct", recorder.Body.String())
	}
}
