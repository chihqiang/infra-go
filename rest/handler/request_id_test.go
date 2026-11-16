package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequestIDHandlerPreservesSafeID(t *testing.T) {
	const requestID = "client-request_42.v1"
	handler := NewRequestIDHandler(RequestIDConfig{Enabled: true}).Handle(func(w http.ResponseWriter, r *http.Request) {
		id, ok := RequestIDFromContext(r.Context())
		if !ok || id != requestID {
			t.Errorf("context request ID = %q, %t; want %q, true", id, ok, requestID)
		}
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, requestID)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get(RequestIDHeader); got != requestID {
		t.Fatalf("response request ID = %q, want %q", got, requestID)
	}
}

func TestRequestIDHandlerGeneratesIDForMissingOrInvalidValue(t *testing.T) {
	for _, incoming := range []string{"", "not a valid id", string(make([]byte, 129))} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set(RequestIDHeader, incoming)
		recorder := httptest.NewRecorder()

		NewRequestIDHandler(RequestIDConfig{Enabled: true}).Handle(func(w http.ResponseWriter, r *http.Request) {
			id, ok := RequestIDFromContext(r.Context())
			if !ok || id != w.Header().Get(RequestIDHeader) {
				t.Errorf("context ID = %q, %t; response ID = %q", id, ok, w.Header().Get(RequestIDHeader))
			}
		}).ServeHTTP(recorder, request)

		id := recorder.Header().Get(RequestIDHeader)
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("incoming ID %q produced invalid generated ID %q: %v", incoming, id, err)
		}
	}
}

func TestContextWithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-1")
	id, ok := RequestIDFromContext(ctx)
	if !ok || id != "req-1" {
		t.Fatalf("request ID = %q, %t; want req-1, true", id, ok)
	}

	empty := ContextWithRequestID(context.Background(), "")
	if _, ok := RequestIDFromContext(empty); ok {
		t.Fatal("empty request ID should not be stored")
	}

	nilCtx := ContextWithRequestID(nil, "req-2")
	if id, ok := RequestIDFromContext(nilCtx); !ok || id != "req-2" {
		t.Fatalf("nil context request ID = %q, %t", id, ok)
	}
	if _, ok := RequestIDFromContext(nil); ok {
		t.Fatal("nil context should not contain a request ID")
	}
}
