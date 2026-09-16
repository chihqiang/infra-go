package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Covers max_conns.go: the concurrency limiting middleware.

func TestMaxConns_DisabledWhenNonPositive(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewMaxConns(0).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMaxConns_RejectsOverLimit(t *testing.T) {
	silenceLogger(t)
	mw := NewMaxConns(1).Middleware()

	release := make(chan struct{})
	slow := func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the only concurrency slot until the test releases it
		w.WriteHeader(http.StatusOK)
	}

	// The first request takes the slot (blocks inside a goroutine)
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		mw(http.HandlerFunc(slow)).ServeHTTP(rec1, req1)
	}()

	// Wait for the slot to be taken (give the goroutine time to be scheduled)
	time.Sleep(20 * time.Millisecond)

	// The second request exceeds the limit → 503
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec2 := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec2.Code)

	// Release the slot so the first request completes
	close(release)
	<-done
	assert.Equal(t, http.StatusOK, rec1.Code)
}

func TestMaxConns_AllowsWithinLimit(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	mw := NewMaxConns(5).Middleware()
	for i := 0; i < 5; i++ {
		rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}
