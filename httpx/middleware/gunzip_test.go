package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 gunzip.go：gzip 请求体自动解压中间件。

// gzipBody 将 body 压缩为 gzip 字节流。
func gzipBody(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestGunzip_DecompressesBody(t *testing.T) {
	compressed := gzipBody(t, "hello gzip body")
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")

	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewGunzip().Middleware(), next, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello gzip body", got)
}

func TestGunzip_IgnoresPlainBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("plain"))
	req.ContentLength = 5

	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewGunzip().Middleware(), next, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "plain", got)
}

func TestGunzip_RejectsInvalidGzip(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-gzip"))
	req.ContentLength = 8
	req.Header.Set("Content-Encoding", "gzip")

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewGunzip().Middleware(), ok, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
