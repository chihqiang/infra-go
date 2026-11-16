package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverHandlerReturnsInternalServerError(t *testing.T) {
	handler := NewRecoveryHandler(RecoveryConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {
		panic("handler failure")
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want an empty body", recorder.Body.String())
	}
}

func TestRecoverHandlerPreservesAbortHandler(t *testing.T) {
	handler := NewRecoveryHandler(RecoveryConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})

	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("panic = %v, want http.ErrAbortHandler", recovered)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestRecoverHandlerAbortsWhenResponseCommitted makes a panic after headers and
// a partial body visible to the client instead of ending the chunked response
// cleanly and looking like a successful (but truncated) reply.
func TestRecoverHandlerAbortsWhenResponseCommitted(t *testing.T) {
	handlerFn := NewRecoveryHandler(RecoveryConfig{Enabled: true}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	})

	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("panic = %v, want http.ErrAbortHandler", recovered)
		}
	}()
	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
