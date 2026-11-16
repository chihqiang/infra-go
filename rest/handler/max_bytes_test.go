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
	handler := NewMaxBytesHandler(MaxBytesConfig{Enabled: true, Limit: 4}).Handle(func(http.ResponseWriter, *http.Request) {
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
	if got := recorder.Header().Get("Connection"); got != "close" {
		t.Fatalf("connection header = %q, want close", got)
	}
}

func TestMaxBytesHandlerLimitsUnknownLengthBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("12345"))
	request.ContentLength = -1
	var readErr error
	handler := NewMaxBytesHandler(MaxBytesConfig{Enabled: true, Limit: 4}).Handle(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("body read error = %v, want *http.MaxBytesError", readErr)
	}
	if got := recorder.Header().Get("Connection"); got != "close" {
		t.Fatalf("connection header = %q, want close", got)
	}
}

func TestMaxBytesHandlerDisablesNonpositiveLimit(t *testing.T) {
	called := false
	handler := NewMaxBytesHandler(MaxBytesConfig{Enabled: true, Limit: 0}).Handle(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("12345")))

	if !called {
		t.Fatal("next handler was not called with a nonpositive limit")
	}
}
