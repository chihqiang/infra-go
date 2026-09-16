package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Covers max_bytes.go: the request body size limiting middleware.

func TestMaxBytes_AllowsWithinLimit(t *testing.T) {
	silenceLogger(t)
	var got []byte
	next := func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 3)
		_, _ = r.Body.Read(b)
		got = b
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("abc"))
	req.ContentLength = 3

	rec := perform(NewMaxBytes(1024).Middleware(), next, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []byte("abc"), got)
}

func TestMaxBytes_RejectsOversized(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestMaxBytes_DisabledWhenNonPositive(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// n <= 0: unlimited, even an oversized body is allowed through
	rec := perform(NewMaxBytes(0).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Equal(t, http.StatusOK, rec.Code)
}
