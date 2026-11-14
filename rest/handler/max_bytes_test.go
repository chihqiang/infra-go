package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMaxBytesHandlerRejectsKnownOversizedBody(t *testing.T) {
	called := false
	handler := MaxBytesHandler(4)(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("12345")))

	if called {
		t.Fatal("next handler was called for an oversized body")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want JSON", recorder.Header().Get("Content-Type"))
	}
}

func TestMaxBytesHandlerLimitsUnknownLengthBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("12345"))
	request.ContentLength = -1
	var readErr error
	handler := MaxBytesHandler(4)(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})

	handler.ServeHTTP(httptest.NewRecorder(), request)

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("body read error = %v, want *http.MaxBytesError", readErr)
	}
}

func TestMaxBytesHandlerDisablesNonpositiveLimit(t *testing.T) {
	called := false
	handler := MaxBytesHandler(0)(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("12345")))

	if !called {
		t.Fatal("next handler was not called with a nonpositive limit")
	}
}
