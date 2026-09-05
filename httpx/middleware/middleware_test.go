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
