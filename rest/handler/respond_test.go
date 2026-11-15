package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorUsesSharedEnvelope(t *testing.T) {
	handlerFn := RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusTooManyRequests, "slow down")
	})
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != jsonContentType {
		t.Fatalf("content type = %q", got)
	}

	var body struct {
		Code      int    `json:"code"`
		Msg       string `json:"msg"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != -1 || body.Msg != "slow down" {
		t.Fatalf("body = %+v", body)
	}
	if body.RequestID == "" {
		t.Fatal("request_id is missing from the error envelope")
	}
}
