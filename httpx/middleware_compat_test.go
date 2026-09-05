package httpx

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/jwt"
	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 转发层冒烟测试：验证 httpx.With* 便捷函数（internal_middleware.go）能正确
// 把 httpx/middleware 子包的标准中间件适配为 httpx.Middleware 并注册到
// Server.Use。核心行为（各中间件逻辑）已在 httpx/middleware 子包逐文件测试，
// 此处仅覆盖代表性中间件的接线，避免适配层回归。

// silenceHttpxLogger 静音全局 logger，避免测试日志污染 stdout/stderr。
func silenceHttpxLogger(t *testing.T) {
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

func TestMiddlewareCompat_Recovery(t *testing.T) {
	silenceHttpxLogger(t)
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

func TestMiddlewareCompat_RequestID(t *testing.T) {
	s := newTestServer()
	s.Use(WithRequestID())
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(middleware.HeaderRequestID))
}

func TestMiddlewareCompat_Cors(t *testing.T) {
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequestWithHeaders(t, s, http.MethodGet, "/ok", nil,
		map[string]string{"Origin": "http://allowed.com"})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddlewareCompat_Timeout(t *testing.T) {
	s := newTestServer()
	s.Use(WithTimeout(30 * time.Millisecond))
	s.AddRoute(Route{
		Method: "GET", Path: "/slow", Handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/slow", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMiddlewareCompat_MaxBytes(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithMaxBytes(4))
	s.AddRoute(Route{
		Method: "POST", Path: "/upload", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodPost, "/upload", strings.NewReader("0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestMiddlewareCompat_ErrorResponseUsesUnifiedJSON(t *testing.T) {
	// 组合 RequestID + Recovery：错误响应应为 httpx 统一 JSON（验证 middleware.SetErrorHandler
	// 在 httpx init 注入的统一 JSON 输出接线正确，而非默认 http.Error 纯文本）
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithRequestID(), WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(middleware.HeaderRequestID)) // RequestID 中间件仍回写响应头
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), "internal server error")
}

func TestMiddlewareCompat_Tracing(t *testing.T) {
	// WithTracing 接线：注册后正常请求 200；ignorePaths 命中路径直接放行
	s := newTestServer()
	s.Use(WithTracing("/health*"))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "health")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")

	recHealth := doRequest(t, s, http.MethodGet, "/health", nil)
	assert.Equal(t, http.StatusOK, recHealth.Code)
	assert.Contains(t, recHealth.Body.String(), "health")
}

func TestMiddlewareCompat_RateLimit(t *testing.T) {
	silenceHttpxLogger(t)
	// WithRateLimit 接线：真实令牌桶 rate=0、容量 1 → 首个请求 200，第二个 429
	s := newTestServer()
	s.Use(WithRateLimit(ratelimit.NewTokenBucket(0, 1)))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)

	rec2 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestMiddlewareCompat_RateLimitSkipPaths(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithRateLimit(ratelimit.NewTokenBucket(0, 1), "/healthz"))
	s.AddRoute(Route{
		Method: "GET", Path: "/healthz", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "health")
		},
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	// 跳过路径不受限流影响（可多次访问）
	rec1 := doRequest(t, s, http.MethodGet, "/healthz", nil)
	rec2 := doRequest(t, s, http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// 非跳过路径：首个通过、第二个被限流
	rec3 := doRequest(t, s, http.MethodGet, "/ok", nil)
	rec4 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec3.Code)
	assert.Equal(t, http.StatusTooManyRequests, rec4.Code)
}
func TestMiddlewareCompat_WithJWT(t *testing.T) {
	silenceHttpxLogger(t)
	j, err := jwt.New(jwt.Config{Secret: "test-secret-key"})
	require.NoError(t, err)

	s := newTestServer()
	s.Use(WithJWT(j, func(r *http.Request) string { return r.Header.Get("X-Token") }))
	s.AddRoute(Route{
		Method: "GET", Path: "/me", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, jwt.ClaimsFromContext(r.Context())[jwt.ClaimKeyUserID])
		},
	})

	// 无 token → 401（httpx 统一 JSON，验证 middleware 错误机制已注入）
	rec := doRequest(t, s, http.MethodGet, "/me", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), "token is missing")

	// 合法 token → 放行，handler 读到业务 claims 中的 user_id
	token, err := j.GenerateAccessToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})
	require.NoError(t, err)
	recOK := doRequestWithHeaders(t, s, http.MethodGet, "/me", nil,
		map[string]string{"X-Token": token})
	assert.Equal(t, http.StatusOK, recOK.Code)
	assert.Contains(t, recOK.Body.String(), "user-123")
}

func TestAsMiddleware_Adapter(t *testing.T) {
	// AsMiddleware 把标准 func(http.Handler) http.Handler 中间件注册进 httpx server。
	// 这里用一个自定义标准中间件（标准 net/http 形态），验证其能被注册并生效。
	s := newTestServer()
	s.Use(AsMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Middleware", "1")
			next.ServeHTTP(w, r)
		})
	}))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Custom-Middleware"))
}
