package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 middleware.go：ErrorHandler 全局注入点，以及各测试文件共享的辅助。

// testKey AES-128 测试密钥（16 字节），供 cryption / content_security 测试复用。
var testKey = []byte("0123456789abcdef")

// perform 用中间件链包装 next 并处理请求，返回响应记录器。
func perform(mw func(http.Handler) http.Handler, next http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	return rec
}

// silenceLogger 把全局 logger 输出重定向到临时文件，避免测试日志污染 stdout/stderr。
// 测试结束后恢复原全局 logger。会触发日志的中间件测试应先调用本函数。
func silenceLogger(t *testing.T) {
	t.Helper()
	tmpLog := filepath.Join(t.TempDir(), "test.log")
	l := logger.New(logger.Config{Output: []string{tmpLog}, Caller: false})
	old := logger.GetGlobal()
	logger.SetGlobal(l)
	t.Cleanup(func() {
		logger.SetGlobal(old)
		_ = l.Sync()
	})
}

// oversizedRequest 构造 Content-Length 超限的 POST 请求（触发 MaxBytes 等限制中间件）。
func oversizedRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	return req
}

// --- ErrorHandler ---

func TestErrorHandler_Default(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec.Body.String(), "request entity too large")
}

func TestErrorHandler_CustomAndRestore(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// 注入自定义错误渲染（JSON）
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"msg":"` + msg + `"}`))
	})

	rec := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"request entity too large"`)

	// SetErrorHandler(nil) 恢复默认 http.Error
	SetErrorHandler(nil)
	rec2 := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Contains(t, rec2.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec2.Body.String(), "request entity too large")
}

// customJSONHandler 用于识别请求作用域渲染是否生效。
func customJSONHandler(_ context.Context, w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"scope":"request","msg":"` + msg + `"}`))
}

// TestErrorHandler_RequestScoped 验证错误渲染可按请求作用域生效，
// 且不污染全局、不影响其它请求（httpx 的 Server 就是靠它在每个请求上注入 JSON 渲染）。
func TestErrorHandler_RequestScoped(t *testing.T) {
	silenceLogger(t)
	SetErrorHandler(nil)
	t.Cleanup(func() { SetErrorHandler(nil) })

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewMaxBytes(4).Middleware()

	// 未注入：默认纯文本
	recDefault := perform(mw, ok, oversizedRequest("0123456789"))
	assert.Contains(t, recDefault.Header().Get("Content-Type"), "text/plain")

	// 请求作用域注入 JSON
	req := oversizedRequest("0123456789")
	req = req.WithContext(ContextWithErrorHandler(req.Context(), customJSONHandler))
	recScoped := perform(mw, ok, req)
	assert.Equal(t, "application/json", recScoped.Header().Get("Content-Type"))
	assert.Contains(t, recScoped.Body.String(), `"scope":"request"`)

	// 全局仍然未被改写：后续其它请求依旧是纯文本
	recAfter := perform(mw, ok, oversizedRequest("0123456789"))
	assert.Contains(t, recAfter.Header().Get("Content-Type"), "text/plain",
		"请求作用域的注入不应泄漏到全局")
}

// TestErrorHandler_RequestScopedOverridesGlobal 验证请求作用域优先于全局。
func TestErrorHandler_RequestScopedOverridesGlobal(t *testing.T) {
	silenceLogger(t)
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	ctx := ContextWithErrorHandler(context.Background(), func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "request")
	})

	rec := httptest.NewRecorder()
	WriteError(ctx, rec, http.StatusForbidden, "x")
	assert.Equal(t, "request", rec.Header().Get("X-Scope"))
}

// TestErrorHandler_GlobalFallback 验证请求未携带渲染函数时回退到全局值。
func TestErrorHandler_GlobalFallback(t *testing.T) {
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, http.StatusForbidden, "x")
	assert.Equal(t, "global", rec.Header().Get("X-Scope"))
}

// TestErrorHandler_NilContext 验证传入 nil context 不会 panic（回退全局）。
func TestErrorHandler_NilContext(t *testing.T) {
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		WriteError(nil, rec, http.StatusForbidden, "x")
	})
	assert.Equal(t, "global", rec.Header().Get("X-Scope"))
}

// TestContextWithErrorHandler_Nil 验证 nil 渲染函数不会写入 context。
func TestContextWithErrorHandler_Nil(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, ContextWithErrorHandler(ctx, nil))
}
