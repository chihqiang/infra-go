package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Covers cors.go: the CORS middleware.

func TestCORS_NoOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewCORS("http://allowed.com").Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_AllowAll(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://any.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// allowAll echoes the concrete Origin (not "*") because Allow-Credentials is sent as well;
	// the "*" + credentials combination is rejected by browsers.
	assert.Equal(t, "http://any.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS, PATCH", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_AllowSpecific(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
}

// TestCORS_UnauthorizedOrigin verifies that an unauthorized origin is **passed through** by
// default: no CORS headers are sent (so the browser blocks scripts from reading the response),
// but the request still reaches the downstream handler.
//
// For why it does not return 403 see the WithRejectUnauthorizedOrigin documentation:
// CORS is a browser-side response reading restriction, not server-side access control; returning
// 403 would hit non-browser clients that send an Origin header (curl / mobile / service calls).
func TestCORS_UnauthorizedOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code, "request must still reach the downstream handler")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"),
		"no CORS header must be sent for a disallowed origin")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORS_RejectUnauthorizedOrigin verifies that the optional strict mode returns 403.
func TestCORS_RejectUnauthorizedOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	mw := NewCORS("http://allowed.com").WithRejectUnauthorizedOrigin(true)
	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_RejectUnauthorizedOrigin_AllowedStillPasses verifies that strict mode does not affect
// allowed origins.
func TestCORS_RejectUnauthorizedOrigin_AllowedStillPasses(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	mw := NewCORS("http://allowed.com").WithRejectUnauthorizedOrigin(true)
	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_UnauthorizedPreflightPassesThrough verifies that a preflight request from an
// unauthorized origin also passes through: with no CORS headers the browser deems the preflight
// to have failed, so the server need not return 403 either.
func TestCORS_UnauthorizedPreflightPassesThrough(t *testing.T) {
	reached := false
	next := func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), next, req)
	assert.True(t, reached, "preflight must reach downstream when not strictly rejecting")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORS_SameOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	// Same-origin: Origin matches the request Host → pass straight through with no CORS headers
	req := httptest.NewRequest(http.MethodGet, "http://example.com/x", nil)
	req.Header.Set("Origin", "http://example.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_OptionsPreflight(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
}
