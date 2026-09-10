package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 cors.go：CORS 中间件。

func TestCORS_NoOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec := perform(NewCORS("http://allowed.com").Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_AllowAll(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://any.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// allowAll 回显具体 Origin（而非 "*"），因为同时下发了 Allow-Credentials；
	// "*" + 凭证的组合会被浏览器拒绝。
	assert.Equal(t, "http://any.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS, PATCH", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_AllowSpecific(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
}

// TestCORS_UnauthorizedOrigin 验证未授权来源默认**透传**：
// 不下发 CORS 头（浏览器会阻止脚本读取响应），但请求仍交给下游。
//
// 不返回 403 的原因见 WithRejectUnauthorizedOrigin 文档：
// CORS 是浏览器侧的响应读取限制，不是服务端准入控制；
// 返回 403 会误伤带 Origin 头的非浏览器客户端（curl / 移动端 / 服务间调用）。
func TestCORS_UnauthorizedOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code, "request must still reach the downstream handler")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"),
		"no CORS header must be sent for a disallowed origin")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORS_RejectUnauthorizedOrigin 验证可选严格模式返回 403。
func TestCORS_RejectUnauthorizedOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	mw := NewCORS("http://allowed.com").WithRejectUnauthorizedOrigin(true)
	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_RejectUnauthorizedOrigin_AllowedStillPasses 验证严格模式不影响授权来源。
func TestCORS_RejectUnauthorizedOrigin_AllowedStillPasses(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	mw := NewCORS("http://allowed.com").WithRejectUnauthorizedOrigin(true)
	rec := perform(mw.Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_UnauthorizedPreflightPassesThrough 验证未授权来源的预检请求也是透传：
// 没有 CORS 头，浏览器会判定预检失败，因此无需服务端再返回 403。
func TestCORS_UnauthorizedPreflightPassesThrough(t *testing.T) {
	reached := false
	next := func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://evil.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), next, req)
	assert.True(t, reached, "preflight must reach downstream when not strictly rejecting")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORS_SameOrigin(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	// 同源：Origin 与请求 Host 一致 → 直接放行且不设 CORS 头
	req := httptest.NewRequest(http.MethodGet, "http://example.com/x", nil)
	req.Header.Set("Origin", "http://example.com")

	rec := perform(NewCORS("*").Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_OptionsPreflight(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "http://allowed.com")

	rec := perform(NewCORS("http://allowed.com").Middleware(), ok, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
}
