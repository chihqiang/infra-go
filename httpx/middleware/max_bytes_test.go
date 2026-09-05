package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 max_bytes.go：请求体大小限制中间件。

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

	// n <= 0：不限制，超大 body 也放行
	rec := perform(NewMaxBytes(0).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Equal(t, http.StatusOK, rec.Code)
}
