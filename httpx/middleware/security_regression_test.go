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

// 本文件覆盖三个安全缺陷的回归测试：
//  1. ContentSecurity 无限制读入请求体，且读取失败时按空体继续校验；
//  2. Gunzip 不限制解压后大小（解压炸弹）；
//  3. CORS 在 allowAll 下同时下发 "*" 与 Allow-Credentials（浏览器拒绝 + 过度授权）。

// --- 1. ContentSecurity ---

// signContentSecurity 按中间件约定生成签名（timestamp\nmethod\npath\nquery\nbodyHex）。
//
// 注意：中间件对请求体**总是**计算 SHA-256（空体也不例外，得到 sha256("")），
// 因此这里也必须始终哈希，不能把空体写成空字符串。
func signContentSecurity(t *testing.T, key []byte, ts, method, path, query, body string) string {
	t.Helper()
	bodyHex := hash.SHA256String(body)
	content := strings.Join([]string{ts, method, path, query, bodyHex}, "\n")
	return hash.HMACSign(key, content)
}

// TestContentSecurity_RawBodyRejected 基线：合法签名通过。
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

// TestContentSecurity_BodyTooLargeRejected 回归测试：超大请求体必须被拒绝而不是全量读入内存。
// 历史缺陷：无限制 io.ReadAll(r.Body)，大 body 可耗尽内存（DoS）。
func TestContentSecurity_BodyTooLargeRejected(t *testing.T) {
	key := []byte("content-security-key")
	const limit = 1024

	// 构造超过上限的 body，并按其真实摘要签名（签名有效，仅体积超限）
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

// TestContentSecurity_BodyAtLimitAccepted 验证恰好等于上限的请求体仍可通过。
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

// failingReader 读取时始终返回错误，用于验证读取失败不会被当作空体。
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingReader) Close() error             { return nil }

// TestContentSecurity_ReadErrorRejected 回归测试：请求体读取失败必须拒绝。
//
// 历史缺陷：`if b, err := io.ReadAll(r.Body); err == nil { ... }` 在 err != nil 时
// 不返回错误，继续以 body="" 参与签名校验 —— 相当于把"读取失败"当作"空体"，
// 请求体是否参与签名变得不可控，完整性约束可被绕过。
func TestContentSecurity_ReadErrorRejected(t *testing.T) {
	key := []byte("content-security-key")

	// 按"空体"签名：旧实现下这个签名会被接受（因为它也用空体计算）
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signContentSecurity(t, key, ts, http.MethodPost, "/api", "", "")

	req := httptest.NewRequest(http.MethodPost, "/api", nil)
	req.Body = failingReader{} // 读取必然失败
	req.Header.Set(ContentSecurityHeader, "time="+ts+"; signature="+sig)

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewContentSecurity(key, time.Minute).Middleware(), ok, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"a body read failure must be rejected, not silently treated as an empty body")
}

// TestContentSecurity_EmptyBodyStillWorks 验证真正的空体请求不受影响。
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

// --- 2. Gunzip 解压炸弹 ---

// gzipOf 将 content 压缩为 gzip 字节流。
func gzipOf(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// TestGunzip_RejectsDecompressionBomb 回归测试：解压后超过上限必须报错。
//
// 历史缺陷：只把 gzip.Reader 赋给 r.Body，不限制解压后大小。
// 高度可压缩的数据（如 10MB 的 'a'）压缩后仅数 KB，
// 任何基于 Content-Length 的限额都会被绕过（zip bomb → OOM）。
func TestGunzip_RejectsDecompressionBomb(t *testing.T) {
	const limit = 4096
	// 压缩后很小、解压后很大
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

// TestGunzip_AllowsBodyWithinLimit 验证上限内的正常解压不受影响。
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

// TestGunzip_ExactLimitAllowed 验证恰好等于上限的解压结果可完整读出。
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

// TestGunzip_ClearsEncodingHeaders 验证解压后 Content-Encoding 被移除、
// ContentLength 被重置（长度已与压缩体无关）。
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

// TestGunzip_DefaultLimitIsBounded 验证默认配置即带上限。
func TestGunzip_DefaultLimitIsBounded(t *testing.T) {
	g := NewGunzip()
	assert.Equal(t, int64(defaultMaxDecompressedBytes), g.maxDecompressedBytes)
}

// --- 3. CORS allowAll + credentials ---

// TestCORS_AllowAllCredentialComboIsBrowserValid 回归测试：allowAll 下不得同时下发
// `Allow-Origin: *` 与 `Allow-Credentials: true`。
//
// 历史缺陷：allowAll 分支固定写 "*"，同时无条件写 Allow-Credentials: true。
// 按 Fetch 规范，携带凭证时不允许通配来源，浏览器会拒绝整个响应 ——
// 即带 withCredentials 的跨域请求在旧实现下必然失败（且属过度授权）。
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

	// 显式断言不变量：永远不能同时出现 "*" 与凭证
	assert.False(t, origin == "*" && creds == "true",
		"Access-Control-Allow-Origin: * must not be combined with credentials")
}

// TestCORS_AllowAllEchoesEachOrigin 验证 allowAll 下不同来源都被各自回显。
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

// TestCORS_WithCredentialsDisabled 验证可关闭凭证下发。
func TestCORS_WithCredentialsDisabled(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").WithCredentials(false).Middleware(), ok, req)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"),
		"credentials must be omitted when disabled")
}

// TestCORS_PreflightUnderAllowAll 验证预检响应同样使用回显来源。
func TestCORS_PreflightUnderAllowAll(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://any.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "http://any.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEqual(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}
