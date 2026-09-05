package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/hash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 cryption.go：请求/响应 AES-GCM 加解密中间件。

func TestCryption_RoundTrip(t *testing.T) {
	silenceLogger(t)
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b) // 回显解密后的明文
	}

	encBody, err := hash.AESGCMEncrypt(testKey, []byte("encrypted-payload"))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req.ContentLength = int64(len(encBody))

	rec := perform(NewCryption(testKey).Middleware(), echo, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 响应体为密文，解密后包含原始 payload
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "encrypted-payload")
}

func TestCryption_InvalidBody(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("not-encrypted"))
	req.ContentLength = 13
	rec := perform(NewCryption(testKey).Middleware(), ok, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCryption_SkipExactPaths(t *testing.T) {
	silenceLogger(t)
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}
	mw := NewCryption(testKey, "/plain").Middleware()

	// 命中跳过：明文透传（不解密、不加密）
	rec := perform(mw, echo, httptest.NewRequest(http.MethodPost, "/plain", strings.NewReader("raw-body")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "raw-body")

	// 未命中：仍按密文处理
	encBody, err := hash.AESGCMEncrypt(testKey, []byte("secret"))
	require.NoError(t, err)
	req2 := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req2.ContentLength = int64(len(encBody))
	rec2 := perform(mw, echo, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec2.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "secret")
}

func TestCryption_SkipPrefixWildcard(t *testing.T) {
	silenceLogger(t)
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}
	mw := NewCryption(testKey, "/public/*").Middleware()

	// 命中通配前缀：明文透传
	rec := perform(mw, echo, httptest.NewRequest(http.MethodPost, "/public/raw", strings.NewReader("open-text")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "open-text")

	// 未命中：仍按密文处理
	encBody, err := hash.AESGCMEncrypt(testKey, []byte("top-secret"))
	require.NoError(t, err)
	req2 := httptest.NewRequest(http.MethodPost, "/secure/data", strings.NewReader(encBody))
	req2.ContentLength = int64(len(encBody))
	rec2 := perform(mw, echo, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec2.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "top-secret")
}
