package handler

import (
	"encoding/json"
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
	if got := recorder.Body.String(); got != "{\"code\":-1,\"msg\":\"Internal Server Error\"}\n" {
		t.Fatalf("body = %q, want the shared error envelope", got)
	}
}

func TestRecoverHandlerEnvelopeCarriesRequestID(t *testing.T) {
	handler := RequestIDHandler(RecoverHandler(func(http.ResponseWriter, *http.Request) {
		panic("handler failure")
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	var body struct {
		Code      int    `json:"code"`
		Msg       string `json:"msg"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != -1 || body.RequestID == "" {
		t.Fatalf("body = %+v, want code -1 and a request_id", body)
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
