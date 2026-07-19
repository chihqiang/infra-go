package httpx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
)

// setupSilentLogger 把全局 logger 输出重定向到临时文件，避免测试日志污染 stdout/stderr。
// 测试结束后恢复原来的全局 logger。
func setupSilentLogger(t *testing.T) {
	t.Helper()
	tmpLog := filepath.Join(t.TempDir(), "test.log")
	l := logger.New(logger.Config{
		Output: []string{tmpLog},
		Caller: false,
	})
	old := logger.GetGlobal()
	logger.SetGlobal(l)
	t.Cleanup(func() {
		logger.SetGlobal(old)
		_ = l.Sync()
	})
}

// --- WithRecovery 测试 ---

func TestWithRecovery_Panic(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
}

func TestWithRecovery_Normal(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestWithRecovery_PanicStillRunsAfter(t *testing.T) {
	// panic 被恢复后，后续请求仍应正常处理（服务器未崩溃）
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}},
	)
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		}},
	)

	// 第一次请求 panic
	rec1 := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec1.Code)

	// 第二次请求正常
	rec2 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "ok")
}

// --- WithLogger 测试 ---

func TestWithLogger_Normal(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithLogger())
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestWithLogger_CapturesErrorStatus(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithLogger())
	s.AddRoute(Route{
		Method: "GET", Path: "/bad", Handler: func(w http.ResponseWriter, r *http.Request) {
			WriteHTTPError(w, http.StatusBadRequest, "bad request")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/bad", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "bad request")
}

func TestWithLogger_CombinedWithRecovery(t *testing.T) {
	// 组合使用：Recovery + Logger，panic 后 Logger 仍能记录
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithRecovery(), WithLogger())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
}

// --- statusRecorder 单元测试 ---

func TestStatusRecorder_DefaultStatus(t *testing.T) {
	// handler 不调 WriteHeader，直接 Write，status 应为 200
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	n, err := rec.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, rec.status, "未显式 WriteHeader 时默认 200")
	assert.Equal(t, 5, rec.bytes)
}

func TestStatusRecorder_CustomStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	rec.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rec.status)
	assert.True(t, rec.wroteHead)
}

func TestStatusRecorder_FirstStatusWins(t *testing.T) {
	// 多次 WriteHeader 只记录第一次（符合 HTTP 规范）
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusTeapot, rec.status)
}

func TestStatusRecorder_AccumulatesBytes(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	_, _ = rec.Write([]byte("abc"))
	_, _ = rec.Write([]byte("defg"))
	assert.Equal(t, 7, rec.bytes)
}

// --- WithCors 测试 ---

// doCorsRequest 发送带 Origin 头的请求，返回响应。
// 显式设置 Host 为 "testserver"，避免 httptest.NewRequest 的默认 Host 导致 origin 被误判为同源。
func doCorsRequest(t *testing.T, s *Server, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "testserver"
	if origin != "" {
		req.Header.Set(corsHeaderOrigin, origin)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestWithCors_NoOrigin(t *testing.T) {
	// 无 Origin 头 → 直接 next，不设置任何 CORS 头
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(corsHeaderAllowOrigin))
}

func TestWithCors_SameOrigin(t *testing.T) {
	// 同源请求（Origin == scheme://host）→ 直接 next，不设置 CORS 头
	s := newTestServer()
	s.Use(WithCors("https://other.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set(corsHeaderOrigin, "http://example.com")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(corsHeaderAllowOrigin))
}

func TestWithCors_AllowAll(t *testing.T) {
	// allowAll="*" → Allow-Origin: *
	s := newTestServer()
	s.Use(WithCors(corsAllowAllOrigins))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://evil.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, corsAllowAllOrigins, rec.Header().Get(corsHeaderAllowOrigin))
}

func TestWithCors_AllowSpecific(t *testing.T) {
	// 指定来源授权 → Allow-Origin: origin，并设置 Vary: Origin
	s := newTestServer()
	s.Use(WithCors("http://allowed.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://allowed.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get(corsHeaderAllowOrigin))
	assert.Equal(t, corsHeaderOrigin, rec.Header().Get(corsHeaderVary))
}

func TestWithCors_Unauthorized(t *testing.T) {
	// 未授权来源 → 403
	s := newTestServer()
	s.Use(WithCors("http://allowed.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://evil.com")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get(corsHeaderAllowOrigin))
}

func TestWithCors_OptionsPreflight(t *testing.T) {
	// OPTIONS 预检（授权来源）→ 204 + CORS 头，handler 不执行
	handlerCalled := false
	s := newTestServer()
	s.Use(WithCors(corsAllowAllOrigins))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}})

	rec := doCorsRequest(t, s, http.MethodOptions, "/test", "http://example.com")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.False(t, handlerCalled, "OPTIONS 预检不应执行 handler")
	assert.Equal(t, corsAllowAllOrigins, rec.Header().Get(corsHeaderAllowOrigin))
	assert.Equal(t, corsDefaultAllowMethods, rec.Header().Get(corsHeaderAllowMethods))
	assert.Equal(t, corsDefaultAllowHeaders, rec.Header().Get(corsHeaderAllowHeaders))
}

func TestWithCors_OptionsUnauthorized(t *testing.T) {
	// 未授权来源的 OPTIONS → 403（不是 204）
	s := newTestServer()
	s.Use(WithCors("http://allowed.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodOptions, "/test", "http://evil.com")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestWithCors_MultipleOrigins(t *testing.T) {
	// 多个授权来源，逐一验证
	s := newTestServer()
	s.Use(WithCors("http://a.com", "http://b.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	// a.com 允许
	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://a.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://a.com", rec.Header().Get(corsHeaderAllowOrigin))

	// b.com 允许
	rec = doCorsRequest(t, s, http.MethodGet, "/test", "http://b.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://b.com", rec.Header().Get(corsHeaderAllowOrigin))

	// c.com 不允许
	rec = doCorsRequest(t, s, http.MethodGet, "/test", "http://c.com")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestWithCors_AllHeadersSet(t *testing.T) {
	// 验证授权请求时所有 CORS 响应头都被正确设置
	s := newTestServer()
	s.Use(WithCors(corsAllowAllOrigins))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://example.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, corsAllowAllOrigins, rec.Header().Get(corsHeaderAllowOrigin))
	assert.Equal(t, corsDefaultAllowMethods, rec.Header().Get(corsHeaderAllowMethods))
	assert.Equal(t, corsDefaultAllowHeaders, rec.Header().Get(corsHeaderAllowHeaders))
	assert.Equal(t, corsDefaultExposeHeaders, rec.Header().Get(corsHeaderExposeHeaders))
	assert.Equal(t, corsDefaultAllowCredentials, rec.Header().Get(corsHeaderAllowCredentials))
	assert.Equal(t, corsDefaultMaxAge, rec.Header().Get(corsHeaderMaxAge))
}

func TestWithCors_AllowAllOverridesList(t *testing.T) {
	// 列表中包含 "*" 时，其余来源被忽略，allowAll 生效
	s := newTestServer()
	s.Use(WithCors("http://a.com", corsAllowAllOrigins, "http://b.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://anyone.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, corsAllowAllOrigins, rec.Header().Get(corsHeaderAllowOrigin))
	// allowAll 时不设 Vary
	assert.Empty(t, rec.Header().Get(corsHeaderVary))
}
