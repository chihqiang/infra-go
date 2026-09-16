package middleware

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/hash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds regression tests for three security defects:
//  1. ContentSecurity read the request body without any limit and kept validating as an empty
//     body when the read failed;
//  2. Gunzip did not limit the size after decompression (decompression bomb);
//  3. CORS sent both "*" and Allow-Credentials under allowAll (the browser rejects it, plus
//     over-authorization).

// --- 1. ContentSecurity ---

// signContentSecurity builds a signature as the middleware expects
// (timestamp\nmethod\npath\nquery\nbodyHex).
//
// Note: the middleware **always** computes SHA-256 over the request body (an empty body is no
// exception and yields sha256("")), so this must always hash too and never write the empty body
// as an empty string.
func signContentSecurity(t *testing.T, key []byte, ts, method, path, query, body string) string {
	t.Helper()
	bodyHex := hash.SHA256String(body)
	content := strings.Join([]string{ts, method, path, query, bodyHex}, "\n")
	return hash.HMACSign(key, content)
}

// TestContentSecurity_RawBodyRejected baseline: a valid signature passes.
func TestContentSecurity_RawBodyRejected(t *testing.T) {
	key := []byte("content-security-key")
	const body = `{"a":1}`
	ts := fmt.Sprintf("%d", time.Now().Unix())

	sig := signContentSecurity(t, key, ts, http.MethodPost, "/api", "", body)
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(body))
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewContentSecurity(key, time.Minute).Middleware(), next, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, got, "body must be restored for downstream handlers")
}

// TestContentSecurity_BodyTooLargeRejected is a regression test: an oversized request body must
// be rejected instead of being read fully into memory.
// Historical defect: an unbounded io.ReadAll(r.Body) let a large body exhaust memory (DoS).
func TestContentSecurity_BodyTooLargeRejected(t *testing.T) {
	key := []byte("content-security-key")
	const limit = 1024

	// build a body over the limit and sign it with its real digest (valid signature, only the
	// size is over the limit)
	body := strings.Repeat("x", limit*3)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signContentSecurity(t, key, ts, http.MethodPost, "/api", "", body)

	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(body))
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewContentSecurity(key, time.Minute).WithMaxBodyBytes(limit)

	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
		"oversized body must be rejected before being fully read")
}

// TestContentSecurity_BodyAtLimitAccepted verifies a body exactly at the limit still passes.
func TestContentSecurity_BodyAtLimitAccepted(t *testing.T) {
	key := []byte("content-security-key")
	const limit = 64
	body := strings.Repeat("y", limit)

	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signContentSecurity(t, key, ts, http.MethodPost, "/api", "", body)

	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(body))
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewContentSecurity(key, time.Minute).WithMaxBodyBytes(limit)

	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code, "body exactly at the limit must be accepted")
}

// failingReader always fails on read, proving a read failure is not treated as an empty body.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingReader) Close() error             { return nil }

// TestContentSecurity_ReadErrorRejected is a regression test: a request body read failure must
// be rejected.
//
// Historical defect: `if b, err := io.ReadAll(r.Body); err == nil { ... }` did not return an
// error when err != nil and kept validating the signature against body="" — effectively
// treating a "read failure" as an "empty body", so whether the body took part in the
// signature became unpredictable and the integrity constraint could be bypassed.
func TestContentSecurity_ReadErrorRejected(t *testing.T) {
	key := []byte("content-security-key")

	// sign as an "empty body": under the old implementation this signature was accepted
	// (because it also computed over the empty body)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signContentSecurity(t, key, ts, http.MethodPost, "/api", "", "")

	req := httptest.NewRequest(http.MethodPost, "/api", nil)
	req.Body = failingReader{} // the read is guaranteed to fail
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewContentSecurity(key, time.Minute).Middleware(), ok, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"a body read failure must be rejected, not silently treated as an empty body")
}

// TestContentSecurity_EmptyBodyStillWorks verifies a genuinely empty body is unaffected.
func TestContentSecurity_EmptyBodyStillWorks(t *testing.T) {
	key := []byte("content-security-key")
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signContentSecurity(t, key, ts, http.MethodGet, "/api", "a=1", "")

	req := httptest.NewRequest(http.MethodGet, "/api?a=1", nil)
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewContentSecurity(key, time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- 2. Gunzip decompression bomb ---

// gzipOf compresses content into a gzip byte stream.
func gzipOf(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// TestGunzip_RejectsDecompressionBomb is a regression test: exceeding the limit after
// decompression must fail.
//
// Historical defect: it merely assigned a gzip.Reader to r.Body without capping the size after
// decompression. Highly compressible data (say 10MB of 'a') compresses down to a few KB, so any
// Content-Length based limit is bypassed (zip bomb -> OOM).
func TestGunzip_RejectsDecompressionBomb(t *testing.T) {
	const limit = 4096
	// tiny once compressed, huge once decompressed
	plain := bytes.Repeat([]byte("a"), limit*100)
	compressed := gzipOf(t, plain)

	t.Logf("compressed=%d bytes, decompressed=%d bytes", len(compressed), len(plain))
	require.Less(t, len(compressed), limit, "compressible payload should be small when compressed")

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")

	var readErr error
	next := func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}

	mw := NewGunzip().WithMaxDecompressedBytes(limit)
	perform(mw.Middleware(), next, req)

	require.Error(t, readErr, "reading past the decompressed limit must fail")
	assert.ErrorIs(t, readErr, errDecompressedTooLarge)
}

// TestGunzip_AllowsBodyWithinLimit verifies normal decompression within the limit is unaffected.
func TestGunzip_AllowsBodyWithinLimit(t *testing.T) {
	plain := []byte("hello gzip body")
	compressed := gzipOf(t, plain)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")

	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewGunzip().Middleware(), next, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello gzip body", got)
}

// TestGunzip_ExactLimitAllowed verifies a decompressed result exactly at the limit is fully
// readable.
func TestGunzip_ExactLimitAllowed(t *testing.T) {
	const limit = 1024
	plain := bytes.Repeat([]byte("z"), limit)
	compressed := gzipOf(t, plain)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")

	var n int
	next := func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		n = len(b)
		w.WriteHeader(http.StatusOK)
	}

	perform(NewGunzip().WithMaxDecompressedBytes(limit).Middleware(), next, req)
	assert.Equal(t, limit, n, "a body exactly at the limit must be fully readable")
}

// TestGunzip_ClearsEncodingHeaders verifies Content-Encoding is removed after decompression and
// ContentLength is reset (the length no longer relates to the compressed body).
func TestGunzip_ClearsEncodingHeaders(t *testing.T) {
	plain := []byte("payload")
	compressed := gzipOf(t, plain)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	req.ContentLength = int64(len(compressed))

	var encHeader string
	var contentLength int64
	next := func(w http.ResponseWriter, r *http.Request) {
		encHeader = r.Header.Get("Content-Encoding")
		contentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}

	perform(NewGunzip().Middleware(), next, req)
	assert.Empty(t, encHeader, "Content-Encoding must be cleared after decompression")
	assert.Equal(t, int64(-1), contentLength, "ContentLength must be reset (unknown)")
}

// TestGunzip_DefaultLimitIsBounded verifies the default configuration already carries a limit.
func TestGunzip_DefaultLimitIsBounded(t *testing.T) {
	g := NewGunzip()
	assert.Equal(t, int64(defaultMaxDecompressedBytes), g.maxDecompressedBytes)
}

// --- 3. CORS allowAll + credentials ---

// TestCORS_AllowAllCredentialComboIsBrowserValid is a regression test: under allowAll,
// `Allow-Origin: *` and `Allow-Credentials: true` must not be sent together.
//
// Historical defect: the allowAll branch always wrote "*" while unconditionally writing
// Allow-Credentials: true. Per the Fetch spec a wildcard origin is not allowed with credentials,
// so the browser rejects the whole response — cross-origin requests with withCredentials always
// failed under the old implementation (and it was over-authorizing).
func TestCORS_AllowAllCredentialComboIsBrowserValid(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://any.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	creds := rec.Header().Get("Access-Control-Allow-Credentials")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotEqual(t, "*", origin, "wildcard origin is invalid together with credentials")
	assert.Equal(t, "http://any.com", origin, "origin must be echoed")
	assert.Equal(t, "true", creds)
	assert.Equal(t, "Origin", rec.Header().Get("Vary"),
		"a reflected origin requires Vary: Origin to avoid cache mixups")

	// explicitly assert the invariant: "*" and credentials must never appear together
	assert.False(t, origin == "*" && creds == "true",
		"Access-Control-Allow-Origin: * must not be combined with credentials")
}

// TestCORS_AllowAllEchoesEachOrigin verifies allowAll echoes each origin back to itself.
func TestCORS_AllowAllEchoesEachOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewCORS("*").Middleware()

	for _, origin := range []string{"http://a.com", "https://b.org:8443", "http://c.io"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Origin", origin)
		rec := perform(mw, ok, req)

		assert.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestCORS_WithCredentialsDisabled verifies credential sending can be turned off.
func TestCORS_WithCredentialsDisabled(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").WithCredentials(false).Middleware(), ok, req)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"),
		"credentials must be omitted when disabled")
}

// TestCORS_PreflightUnderAllowAll verifies preflight responses use the echoed origin as well.
func TestCORS_PreflightUnderAllowAll(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://any.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "http://any.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEqual(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}
