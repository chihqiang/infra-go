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

// Covers cryption.go: the request/response AES-GCM encryption middleware.

func TestCryption_RoundTrip(t *testing.T) {
	silenceLogger(t)
	echo := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b) // echo back the decrypted plaintext
	}

	encBody, err := hash.AESGCMEncrypt(testKey, []byte("encrypted-payload"))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req.ContentLength = int64(len(encBody))

	rec := perform(NewCryption(testKey).Middleware(), echo, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// The response body is ciphertext; once decrypted it contains the original payload
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

	// Skip rule matched: plaintext pass-through (no decryption, no encryption)
	rec := perform(mw, echo, httptest.NewRequest(http.MethodPost, "/plain", strings.NewReader("raw-body")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "raw-body")

	// Not matched: still handled as ciphertext
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

	// Wildcard prefix matched: plaintext pass-through
	rec := perform(mw, echo, httptest.NewRequest(http.MethodPost, "/public/raw", strings.NewReader("open-text")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "open-text")

	// Not matched: still handled as ciphertext
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

// --- Status codes and response policy ---

func TestCryption_ErrorResponsePlaintext(t *testing.T) {
	silenceLogger(t)
	notFound := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}
	rec := perform(NewCryption(testKey).Middleware(), notFound,
		httptest.NewRequest(http.MethodGet, "/missing", nil))

	// Not 2xx: plaintext pass-through that keeps the status code (clients can read the error
	// without decrypting anything)
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

	// 204 means no body: an encrypted body must not be emitted
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

	// HEAD: the status code is preserved but there is no response body
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
	req.ContentLength = -1 // Transfer-Encoding: chunked (ContentLength == -1 in Go)

	rec := perform(NewCryption(testKey).Middleware(), echo, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "chunked-payload")
}

func TestCryption_RequestTooLarge(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// Build a request body that exceeds the 16 byte limit
	big := strings.Repeat("A", 32)
	encBody, err := hash.AESGCMEncrypt(testKey, []byte(big))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	req.ContentLength = int64(len(encBody))

	rec := perform(NewCryptionWithLimit(testKey, 16, defaultMaxBytes).Middleware(), ok, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestCryption_ResponseOverflowFallsBackPlaintext(t *testing.T) {
	silenceLogger(t)
	// A response exceeding the default buffer limit (5MB) must fall back to plaintext: write it in
	// chunks so that the leading part is buffered and the rest trips the limit
	const chunk = 512 * 1024
	big := func(w http.ResponseWriter, r *http.Request) {
		blob := bytes.Repeat([]byte("x"), chunk)
		for i := 0; i < 12; i++ { // 6MB in total > the 5MB limit
			_, _ = w.Write(blob)
		}
	}
	rec := perform(NewCryption(testKey).Middleware(), big,
		httptest.NewRequest(http.MethodGet, "/big", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	// Plaintext fallback: all 12 chunks = 6MB of plaintext are written out (the body is directly
	// readable, not ciphertext) and must not be truncated
	assert.Len(t, rec.Body.Bytes(), 12*chunk)
	assert.Equal(t, bytes.Repeat([]byte("x"), chunk), rec.Body.Bytes()[:chunk])
}

func TestCryption_ResponseLimitConfigurable(t *testing.T) {
	silenceLogger(t)
	// A custom, smaller response limit (64KB) to verify that the configuration takes effect
	const customLimit = 64 * 1024
	const chunk = 32 * 1024
	big := func(w http.ResponseWriter, r *http.Request) {
		blob := bytes.Repeat([]byte("y"), chunk)
		for i := 0; i < 4; i++ { // 128KB in total > the 64KB limit
			_, _ = w.Write(blob)
		}
	}
	mw := NewCryptionWithLimit(testKey, defaultMaxBytes, customLimit)
	rec := perform(mw.Middleware(), big, httptest.NewRequest(http.MethodGet, "/big", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, rec.Body.Bytes(), 4*chunk) // plaintext fallback: all 128KB written, no truncation
}

func TestCryption_SuccessResponseEncryptedWithStatus(t *testing.T) {
	silenceLogger(t)
	created := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("resource-1"))
	}
	rec := perform(NewCryption(testKey).Middleware(), created,
		httptest.NewRequest(http.MethodGet, "/create", nil))

	// 2xx (201) keeps the status code and encrypts the body
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "resource-1")
}
