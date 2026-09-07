package middleware

import (
	"bytes"
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

// --- 状态码与响应策略 ---

func TestCryption_ErrorResponsePlaintext(t *testing.T) {
	silenceLogger(t)
	notFound := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}
	rec := perform(NewCryption(testKey).Middleware(), notFound,
		httptest.NewRequest(http.MethodGet, "/missing", nil))

	// 非 2xx：明文透传且保留状态码（客户端可直接读取错误，无需解密）
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "not found")
}

func TestCryption_NoContentPlaintext(t *testing.T) {
	silenceLogger(t)
	noContent := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
	rec := perform(NewCryption(testKey).Middleware(), noContent,
		httptest.NewRequest(http.MethodGet, "/empty", nil))

	// 204 无 body 语义：不得输出密文 body
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestCryption_HeadNoBody(t *testing.T) {
	silenceLogger(t)
	head := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("would-be-body"))
	}
	rec := perform(NewCryption(testKey).Middleware(), head,
		httptest.NewRequest(http.MethodHead, "/probe", nil))

	// HEAD：保留状态码但无响应体
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestCryption_ChunkedRequestBody(t *testing.T) {
	silenceLogger(t)
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}

	encBody, err := hash.AESGCMEncrypt(testKey, []byte("chunked-payload"))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req.ContentLength = -1 // Transfer-Encoding: chunked（Go 中 ContentLength==-1）

	rec := perform(NewCryption(testKey).Middleware(), echo, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "chunked-payload")
}

func TestCryption_RequestTooLarge(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// 构造超过 16 字节上限的请求体
	big := strings.Repeat("A", 32)
	encBody, err := hash.AESGCMEncrypt(testKey, []byte(big))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req.ContentLength = int64(len(encBody))

	rec := perform(NewCryptionWithLimit(testKey, 16).Middleware(), ok, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestCryption_ResponseOverflowFallsBackPlaintext(t *testing.T) {
	silenceLogger(t)
	// 响应超过缓冲上限（1MB）应回退明文：分块写入，使前段进入缓冲、后续触发超限
	const chunk = 256 * 1024
	big := func(w http.ResponseWriter, r *http.Request) {
		blob := bytes.Repeat([]byte("x"), chunk)
		for i := 0; i < 6; i++ { // 共 1.5MB > 1MB 上限
			_, _ = w.Write(blob)
		}
	}
	rec := perform(NewCryption(testKey).Middleware(), big,
		httptest.NewRequest(http.MethodGet, "/big", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	// 明文回退：输出已缓冲的 1MB 明文，body 可直接读取（非密文）
	assert.Len(t, rec.Body.Bytes(), maxEncryptedResponseBytes)
}

func TestCryption_SuccessResponseEncryptedWithStatus(t *testing.T) {
	silenceLogger(t)
	created := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("resource-1"))
	}
	rec := perform(NewCryption(testKey).Middleware(), created,
		httptest.NewRequest(http.MethodGet, "/create", nil))

	// 2xx（201）保留状态码，body 加密
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "resource-1")
}
