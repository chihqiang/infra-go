package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Covers breaker.go: the global circuit breaker middleware.

func TestBreaker_Allows(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewBreaker().Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBreaker_RejectsOnFailure(t *testing.T) {
	silenceLogger(t)
	brk := NewBreaker().Middleware()
	fail := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }

	// Once a burst of 5xx responses trips the breaker, requests are rejected quickly (503)
	var rejected bool
	for i := 0; i < 2000; i++ {
		rec := perform(brk, fail, httptest.NewRequest(http.MethodGet, "/fail", nil))
		if rec.Code == http.StatusServiceUnavailable {
			rejected = true
			break
		}
	}
	assert.True(t, rejected, "the breaker should eventually open and reject requests")
}
