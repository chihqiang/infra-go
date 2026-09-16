package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Covers timeout.go: the request timeout middleware.

func TestTimeout_DisabledWhenNonPositive(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewTimeout(0).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTimeout_CompletesWithinDeadline(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // shorter than the timeout
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewTimeout(time.Second).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTimeout_TimesOutSlowHandler(t *testing.T) {
	slow := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewTimeout(30*time.Millisecond).Middleware(), slow,
		httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTimeout_WebSocketExempt(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// WebSocket upgrade requests are exempt from the timeout: they pass straight through even
	// past the deadline (this case verifies no timeout write is triggered)
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := perform(NewTimeout(1*time.Millisecond).Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestTimeout_StreamingPreservesStatusCode verifies that a streaming handler (explicit status
// code + Flush) does not have its status code overwritten by an implicit 200.
func TestTimeout_StreamingPreservesStatusCode(t *testing.T) {
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	rec := perform(NewTimeout(time.Second).Middleware(), stream,
		httptest.NewRequest(http.MethodGet, "/stream", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "boom", rec.Body.String())
}

// TestTimeout_StreamingChunkedWrites verifies that a streamed response with several
// Write+Flush calls keeps its full body and correct status code.
func TestTimeout_StreamingChunkedWrites(t *testing.T) {
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		f, _ := w.(http.Flusher)
		for _, chunk := range []string{"a", "b", "c"} {
			_, _ = w.Write([]byte(chunk))
			if f != nil {
				f.Flush()
			}
		}
	}

	rec := perform(NewTimeout(time.Second).Middleware(), stream,
		httptest.NewRequest(http.MethodGet, "/stream", nil))
	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "abc", rec.Body.String())
}
