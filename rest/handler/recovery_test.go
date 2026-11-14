package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverHandlerReturnsInternalServerError(t *testing.T) {
	handler := RecoverHandler(func(http.ResponseWriter, *http.Request) {
		panic("handler failure")
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() != "Internal Server Error\n" {
		t.Fatalf("body = %q, want internal server error", recorder.Body.String())
	}
}

func TestRecoverHandlerPreservesAbortHandler(t *testing.T) {
	handler := RecoverHandler(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})

	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("panic = %v, want http.ErrAbortHandler", recovered)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
