package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 cors.go：CORS 中间件。

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
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
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

func TestCORS_UnauthorizedOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCORS_SameOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	// 同源：Origin 与请求 Host 一致 → 直接放行且不设 CORS 头
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
