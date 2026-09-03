package httpx

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/hash"
	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// newFileLogger 将全局 logger 重定向到临时文件并返回其句柄与路径，
// 便于在测试中断言日志是否真的被写入。
func newFileLogger(t *testing.T) (logger.ILogger, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "test.log")
	l := logger.New(logger.Config{
		Output: []string{logPath},
		Caller: false,
	})
	old := logger.GetGlobal()
	logger.SetGlobal(l)
	t.Cleanup(func() {
		logger.SetGlobal(old)
		_ = l.Sync()
	})
	return l, logPath
}

// readLogLines 读取日志文件内容并返回非空行。
// 文件不存在（未产生任何日志）时返回空切片。
func readLogLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestWithLogger_SkipExactPaths(t *testing.T) {
	l, logPath := newFileLogger(t)
	s := newTestServer()
	s.Use(WithLogger("/skip", "/skip2"))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		}},
	)
	s.AddRoute(Route{
		Method: "GET", Path: "/skip", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "skip")
		}},
	)
	s.AddRoute(Route{
		Method: "GET", Path: "/skip2", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "skip2")
		}},
	)

	// 被跳过的路径：业务照常处理但不写访问日志
	for _, p := range []string{"/skip", "/skip2"} {
		rec := doRequest(t, s, http.MethodGet, p, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	_ = l.Sync()
	assert.Empty(t, readLogLines(t, logPath))

	// 未跳过的路径仍正常记录
	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	_ = l.Sync()
	lines := readLogLines(t, logPath)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "http request")
	assert.Contains(t, lines[0], "/ok")
}

func TestWithLogger_SkipPrefixWildcard(t *testing.T) {
	l, logPath := newFileLogger(t)
	s := newTestServer()
	s.Use(WithLogger("/internal/*"))
	internal := func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "internal") }
	s.AddRoute(Route{Method: "GET", Path: "/internal/health", Handler: internal})
	s.AddRoute(Route{Method: "GET", Path: "/internal/a/b", Handler: internal})
	s.AddRoute(Route{
		Method: "GET", Path: "/api/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "users")
		}},
	)

	// 命中通配前缀的路径：不写访问日志
	for _, p := range []string{"/internal/health", "/internal/a/b"} {
		rec := doRequest(t, s, http.MethodGet, p, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	_ = l.Sync()
	assert.Empty(t, readLogLines(t, logPath))

	// 未命中通配的路径正常记录
	rec := doRequest(t, s, http.MethodGet, "/api/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	_ = l.Sync()
	lines := readLogLines(t, logPath)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "/api/users")
}

// --- WithCors 测试 ---

// doCorsRequest 发送带 Origin 头的请求，返回响应。
// 显式设置 Host 为 "testserver"，避免 httptest.NewRequest 的默认 Host 导致 origin 被误判为同源。
func doCorsRequest(t *testing.T, s *Server, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "testserver"
	if origin != "" {
		req.Header.Set("Origin", origin)
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
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestWithCors_SameOrigin(t *testing.T) {
	// 同源请求（Origin == scheme://host）→ 直接 next，不设置 CORS 头
	s := newTestServer()
	s.Use(WithCors("https://other.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestWithCors_AllowAll(t *testing.T) {
	// allowAll="*" → Allow-Origin: *
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://evil.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestWithCors_AllowSpecific(t *testing.T) {
	// 指定来源授权 → Allow-Origin: origin，并设置 Vary: Origin
	s := newTestServer()
	s.Use(WithCors("http://allowed.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://allowed.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
}

func TestWithCors_Unauthorized(t *testing.T) {
	// 未授权来源 → 403
	s := newTestServer()
	s.Use(WithCors("http://allowed.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://evil.com")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestWithCors_OptionsPreflight(t *testing.T) {
	// OPTIONS 预检（授权来源）→ 204 + CORS 头，handler 不执行
	handlerCalled := false
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}})

	rec := doCorsRequest(t, s, http.MethodOptions, "/test", "http://example.com")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.False(t, handlerCalled, "OPTIONS 预检不应执行 handler")
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS, PATCH", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization, X-Requested-With, Accept, Origin", rec.Header().Get("Access-Control-Allow-Headers"))
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
	assert.Equal(t, "http://a.com", rec.Header().Get("Access-Control-Allow-Origin"))

	// b.com 允许
	rec = doCorsRequest(t, s, http.MethodGet, "/test", "http://b.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://b.com", rec.Header().Get("Access-Control-Allow-Origin"))

	// c.com 不允许
	rec = doCorsRequest(t, s, http.MethodGet, "/test", "http://c.com")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestWithCors_AllHeadersSet(t *testing.T) {
	// 验证授权请求时所有 CORS 响应头都被正确设置
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://example.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS, PATCH", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization, X-Requested-With, Accept, Origin", rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "Content-Length, Content-Type", rec.Header().Get("Access-Control-Expose-Headers"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "86400", rec.Header().Get("Access-Control-Max-Age"))
}

func TestWithCors_AllowAllOverridesList(t *testing.T) {
	// 列表中包含 "*" 时，其余来源被忽略，allowAll 生效
	s := newTestServer()
	s.Use(WithCors("http://a.com", "*", "http://b.com"))
	s.AddRoute(Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") }})

	rec := doCorsRequest(t, s, http.MethodGet, "/test", "http://anyone.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	// allowAll 时不设 Vary
	assert.Empty(t, rec.Header().Get("Vary"))
}

// --- WithRequestID 测试 ---

func TestWithRequestID_FromHeader(t *testing.T) {
	s := newTestServer()
	s.Use(WithRequestID())
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSONCtx(r.Context(), w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderRequestID, "req-abc")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, "req-abc", rec.Header().Get(HeaderRequestID))
	assert.Contains(t, rec.Body.String(), `"request_id":"req-abc"`)
}

func TestWithRequestID_Generate(t *testing.T) {
	s := newTestServer()
	s.Use(WithRequestID())
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSONCtx(r.Context(), w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/test", nil)

	id := rec.Header().Get(HeaderRequestID)
	assert.NotEmpty(t, id)
	assert.Contains(t, rec.Body.String(), `"request_id":"`+id+`"`)
}

func TestWithRequestID_OverridesResponseHeaderOnRebuild(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSONCtx(r.Context(), w, "ok")
		},
	})

	// 未使用中间件时不注入
	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Empty(t, rec.Header().Get(HeaderRequestID))
	assert.NotContains(t, rec.Body.String(), "request_id")
}

// --- WithBreaker 测试 ---

func TestWithBreaker_Allows(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithBreaker())
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithBreaker_RejectsOnFailure(t *testing.T) {
	setupSilentLogger(t)
	s := newTestServer()
	s.Use(WithBreaker())
	s.AddRoute(Route{
		Method: "GET", Path: "/fail", Handler: func(w http.ResponseWriter, r *http.Request) {
			WriteHTTPError(w, http.StatusInternalServerError, "boom")
		},
	})

	// 大量 5xx 触发熔断后，请求被快速拒绝（503）
	var rejected bool
	for i := 0; i < 2000; i++ {
		rec := doRequest(t, s, http.MethodGet, "/fail", nil)
		if rec.Code == http.StatusServiceUnavailable {
			rejected = true
			break
		}
	}
	assert.True(t, rejected, "熔断器应最终打开并拒绝请求")
}

// --- WithTimeout 测试 ---

func TestWithTimeout_DisabledWhenNonPositive(t *testing.T) {
	s := newTestServer()
	s.Use(WithTimeout(0))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithTimeout_CompletesWithinDeadline(t *testing.T) {
	s := newTestServer()
	s.Use(WithTimeout(time.Second))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestWithTimeout_TimesOutSlowHandler(t *testing.T) {
	s := newTestServer()
	s.Use(WithTimeout(50 * time.Millisecond))
	s.AddRoute(Route{
		Method: "GET", Path: "/slow", Handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			OkJSON(w, "done")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/slow", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// --- WithMaxBytes 测试 ---

func TestWithMaxBytes_AllowsWithinLimit(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxBytes(1024))
	s.AddRoute(Route{
		Method: "POST", Path: "/upload", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("small body"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithMaxBytes_RejectsOversized(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxBytes(10))
	s.AddRoute(Route{
		Method: "POST", Path: "/upload", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("this body is way too long"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestWithMaxBytes_DisabledWhenNonPositive(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxBytes(0))
	s.AddRoute(Route{
		Method: "POST", Path: "/upload", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("any size"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- WithGunzip 测试 ---

func TestWithGunzip_DecompressesBody(t *testing.T) {
	s := newTestServer()
	s.Use(WithGunzip())
	s.AddRoute(Route{
		Method: "POST", Path: "/gz", Handler: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			OkJSON(w, string(body))
		},
	})

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte("hello gzip"))
	_ = gw.Close()

	req := httptest.NewRequest(http.MethodPost, "/gz", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hello gzip")
}

func TestWithGunzip_IgnoresPlainBody(t *testing.T) {
	s := newTestServer()
	s.Use(WithGunzip())
	s.AddRoute(Route{
		Method: "POST", Path: "/plain", Handler: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			OkJSON(w, string(body))
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/plain", strings.NewReader("plain body"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "plain body")
}

func TestWithGunzip_RejectsInvalidGzip(t *testing.T) {
	s := newTestServer()
	s.Use(WithGunzip())
	s.AddRoute(Route{
		Method: "POST", Path: "/gz", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/gz", strings.NewReader("not gzip data"))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- WithMaxConns 测试 ---

func TestWithMaxConns_DisabledWhenNonPositive(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxConns(0))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	for i := 0; i < 5; i++ {
		rec := doRequest(t, s, http.MethodGet, "/ok", nil)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestWithMaxConns_AllowsWithinLimit(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxConns(2))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithMaxConns_RejectsOverLimit(t *testing.T) {
	s := newTestServer()
	s.Use(WithMaxConns(1))
	s.AddRoute(Route{
		Method: "GET", Path: "/slow", Handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			OkJSON(w, "ok")
		},
	})

	// 第一个请求占用唯一信号量
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/slow", nil)
			s.Handler().ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}
	wg.Wait()
	close(results)

	// 并发数为 1，至少有一个请求被 503 拒绝
	var rejected bool
	for code := range results {
		if code == http.StatusServiceUnavailable {
			rejected = true
		}
	}
	assert.True(t, rejected, "并发超限应返回 503")
}

// --- WithCryption 测试 ---

var testKey = []byte("0123456789abcdef") // 16 字节，AES-128

func TestWithCryption_RoundTrip(t *testing.T) {
	s := newTestServer()
	s.Use(WithCryption(testKey))
	s.AddRoute(Route{
		Method: "POST", Path: "/echo", Handler: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			OkJSON(w, string(body))
		},
	})

	// 加密请求体
	encBody, err := hash.AESGCMEncrypt(testKey, []byte("encrypted-payload"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(encBody))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// 响应体是加密的，解密后包含原始 payload
	dec, err := hash.AESGCMDecrypt(testKey, rec.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "encrypted-payload")
}

func TestWithCryption_InvalidBody(t *testing.T) {
	s := newTestServer()
	s.Use(WithCryption(testKey))
	s.AddRoute(Route{
		Method: "POST", Path: "/echo", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("not-encrypted"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// echoHandler 返回请求体原文，用于验证加解密路径。
func echoHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	OkJSON(w, string(body))
}

func TestWithCryption_SkipExactPaths(t *testing.T) {
	s := newTestServer()
	s.Use(WithCryption(testKey, "/plain"))
	s.AddRoute(Route{Method: "POST", Path: "/plain", Handler: echoHandler})
	s.AddRoute(Route{Method: "POST", Path: "/echo", Handler: echoHandler})

	// 命中跳过规则的路径：明文透传（不解密请求体、不加密响应）
	rec := doRequest(t, s, http.MethodPost, "/plain", strings.NewReader("raw-body"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "raw-body")

	// 未被跳过的路径：仍按密文处理（响应体加密）
	encBody, err := hash.AESGCMEncrypt(testKey, []byte("secret"))
	require.NoError(t, err)
	rec2 := doRequest(t, s, http.MethodPost, "/echo", strings.NewReader(encBody))
	assert.Equal(t, http.StatusOK, rec2.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec2.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "secret")
}

func TestWithCryption_SkipPrefixWildcard(t *testing.T) {
	s := newTestServer()
	s.Use(WithCryption(testKey, "/public/*"))
	s.AddRoute(Route{Method: "POST", Path: "/public/raw", Handler: echoHandler})
	s.AddRoute(Route{Method: "POST", Path: "/secure/data", Handler: echoHandler})

	// 命中通配前缀：明文透传
	rec := doRequest(t, s, http.MethodPost, "/public/raw", strings.NewReader("open-text"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "open-text")

	// 未命中通配前缀：仍按密文处理
	encBody, err := hash.AESGCMEncrypt(testKey, []byte("top-secret"))
	require.NoError(t, err)
	rec2 := doRequest(t, s, http.MethodPost, "/secure/data", strings.NewReader(encBody))
	assert.Equal(t, http.StatusOK, rec2.Code)
	dec, err := hash.AESGCMDecrypt(testKey, rec2.Body.String())
	require.NoError(t, err)
	assert.Contains(t, string(dec), "top-secret")
}

// --- WithContentSecurity 测试 ---

// buildSignedRequest 构造带合法签名的请求。
func buildSignedRequest(t *testing.T, key []byte, method, path string, body string, ts int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// 签名内容：timestamp\nmethod\npath\nquery\nbodySha256Hex
	signContent := fmt.Sprintf("%d\n%s\n%s\n%s\n%s",
		ts, method, "/"+strings.TrimPrefix(path, "/"), "", bodySHA256Hex(body))
	signature := hash.HMACSign(key, signContent)
	req.Header.Set(ContentSecurityHeader,
		fmt.Sprintf("time=%d; signature=%s", ts, signature))
	return req
}

func TestWithContentSecurity_Valid(t *testing.T) {
	s := newTestServer()
	s.Use(WithContentSecurity(testKey, 5*time.Minute))
	s.AddRoute(Route{
		Method: "POST", Path: "/data", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := buildSignedRequest(t, testKey, http.MethodPost, "/data", `{"a":1}`, time.Now().Unix())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithContentSecurity_InvalidSignature(t *testing.T) {
	s := newTestServer()
	s.Use(WithContentSecurity(testKey, 5*time.Minute))
	s.AddRoute(Route{
		Method: "POST", Path: "/data", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	// 错误密钥签名 → 401
	req := buildSignedRequest(t, []byte("wrong-key-1234567"), http.MethodPost, "/data", "x", time.Now().Unix())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWithContentSecurity_Expired(t *testing.T) {
	s := newTestServer()
	s.Use(WithContentSecurity(testKey, 5*time.Minute))
	s.AddRoute(Route{
		Method: "POST", Path: "/data", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	// 时间戳在 10 分钟前（超出 5 分钟容差）→ 403 防重放
	req := buildSignedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Add(-10*time.Minute).Unix())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestWithContentSecurity_MissingHeader(t *testing.T) {
	s := newTestServer()
	s.Use(WithContentSecurity(testKey, 5*time.Minute))
	s.AddRoute(Route{
		Method: "POST", Path: "/data", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/data", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// --- 辅助 ---

// bodySHA256Hex 计算请求体的 SHA-256 十六进制摘要（用于构造签名）。
func bodySHA256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
