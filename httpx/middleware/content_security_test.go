package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/hash"
	"github.com/stretchr/testify/assert"
)

// 对应 content_security.go：内容安全校验中间件（防篡改 + 防重放）。

// bodySHA256Hex 计算请求体的 SHA-256 十六进制摘要（用于构造签名）。
func bodySHA256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// signedRequest 构造带合法签名的请求。
func signedRequest(t *testing.T, key []byte, method, path, body string, ts int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// 签名内容：timestamp\nmethod\npath\nquery\nbodySha256Hex
	signContent := fmt.Sprintf("%d\n%s\n%s\n%s\n%s",
		ts, method, "/"+strings.TrimPrefix(path, "/"), "", bodySHA256Hex(body))
	signature := hash.HMACSign(key, signContent)
	req.Header.Set(ContentSecurityHeader, fmt.Sprintf("time=%d; signature=%s", ts, signature))
	return req
}

func TestContentSecurity_Valid(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	req := signedRequest(t, testKey, http.MethodPost, "/data", `{"a":1}`, time.Now().Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContentSecurity_InvalidSignature(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// 错误密钥签名 → 401
	req := signedRequest(t, []byte("wrong-key-1234567"), http.MethodPost, "/data", "x", time.Now().Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestContentSecurity_Expired(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// 时间戳在 10 分钟前（超出 5 分钟容差）→ 403 防重放
	req := signedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Add(-10*time.Minute).Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestContentSecurity_MissingHeader(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	req := httptest.NewRequest(http.MethodPost, "/data", nil)
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestContentSecurity_InvalidTimestamp(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// timestamp 非数字 → 401
	req := signedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Unix())
	req.Header.Set(ContentSecurityHeader, "time=abc; signature=xyz")
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
