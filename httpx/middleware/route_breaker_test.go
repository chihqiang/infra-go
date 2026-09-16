package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Covers route_breaker.go: the middleware that isolates circuit breaking per route.

func TestRouteBreaker_Allows(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewRouteBreaker().Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouteBreaker_Isolation(t *testing.T) {
	silenceLogger(t)
	// under one middleware instance: breaking /fail does not affect /ok (isolated by METHOD:path)
	mw := NewRouteBreaker().Middleware()
	fail := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	var rejected bool
	for i := 0; i < 2000; i++ {
		if rec := perform(mw, fail, httptest.NewRequest(http.MethodGet, "/fail", nil)); rec.Code == http.StatusServiceUnavailable {
			rejected = true
			break
		}
	}
	assert.True(t, rejected, "/fail must eventually be circuit-broken")

	// isolation works: /ok still goes through normally
	rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
