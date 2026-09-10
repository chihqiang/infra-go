package httpx

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Server 可变状态的并发安全（回归）：
//   - routes：AddRoutes 写入、Routes/PrintRoutes 读取
//   - httpServer：Start 写入、Shutdown/Stop 读取
// 两者此前均无锁保护，-race 可检出数据竞争。

func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}

// TestServer_RoutesConcurrentWithAddRoute 回归测试：并发 AddRoute 与 Routes 不得竞争。
func TestServer_RoutesConcurrentWithAddRoute(t *testing.T) {
	s := newTestServer()

	const n = 200
	var wg sync.WaitGroup

	// 并发写：注册路由（每个 goroutine 使用独立路径前缀，
	// 因为 http.ServeMux 对重复 pattern 会 panic）
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				s.AddRoute(Route{
					Method:  "GET",
					Path:    fmt.Sprintf("/g%d/r%d", id, j),
					Handler: okHandler(),
				})
			}
		}(i)
	}

	// 并发读：Routes / routesSnapshot
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				_ = s.Routes()
				_ = s.routesSnapshot()
			}
		}()
	}

	wg.Wait()
	assert.Len(t, s.Routes(), 4*n)
}

// TestServer_RoutesSnapshotIsACopy 验证 Routes 返回副本，修改不影响内部状态。
func TestServer_RoutesSnapshotIsACopy(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{Method: "GET", Path: "/a", Handler: okHandler()})

	got := s.Routes()
	require.Len(t, got, 1)

	got[0].Path = "/mutated"
	assert.Equal(t, "/a", s.Routes()[0].Path, "mutating the returned slice must not affect the server")
}

// TestServer_StopConcurrentWithStart 回归测试：Stop/Shutdown 与 Start 并发不得竞争
// httpServer 字段。
//
// 历史缺陷：Start 无锁写 s.httpServer，Shutdown/Stop 无锁读，
// -race 可检出（service.ServiceGroup 会在不同 goroutine 中启停）。
func TestServer_StopConcurrentWithStart(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})
	s.AddRoute(Route{Method: "GET", Path: "/ok", Handler: okHandler()})

	var wg sync.WaitGroup
	// 并发调用 Stop/Shutdown（读取 httpServer）
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Stop()
		}()
	}
	// 并发模拟 Start 阶段的登记（写入 httpServer）
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.stateLk.Lock()
			s.httpServer = &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(okHandler())}
			s.stateLk.Unlock()
		}()
	}
	wg.Wait()
}

// TestServer_StopBeforeStartIsNoop 验证 Start 未调用时 Stop/Shutdown 是空操作且不 panic。
func TestServer_StopBeforeStartIsNoop(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})

	assert.Nil(t, s.currentHTTPServer())
	require.NotPanics(t, func() {
		assert.NoError(t, s.Shutdown())
		assert.NoError(t, s.Stop())
	})
}

// TestServer_ShutdownAfterRegister 验证登记后 Shutdown 会真正作用于该实例。
func TestServer_ShutdownAfterRegister(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(okHandler())}

	s.stateLk.Lock()
	s.httpServer = srv
	s.stateLk.Unlock()

	require.NotNil(t, s.currentHTTPServer())
	// 未启动的 http.Server 调用 Shutdown 返回 ErrServerClosed 之外的结果或 nil，
	// 此处只断言不 panic 且不误报为 nil 分支
	require.NotPanics(t, func() { _ = s.Shutdown() })
}

// TestServer_ConcurrentRoutesAndHandler 验证路由注册与 Handler 构建并发安全。
func TestServer_ConcurrentRoutesAndHandler(t *testing.T) {
	s := newTestServer()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.AddRoute(Route{
					Method:  "GET",
					Path:    fmt.Sprintf("/h%d/r%d", id, j),
					Handler: okHandler(),
				})
				_ = s.Handler()
			}
		}(i)
	}
	wg.Wait()
	require.NotNil(t, s.Handler())
}

// --- 路由模板注入（供按路由聚合的中间件使用）---

// TestServer_GlobalMiddlewareSeesRoutePattern 回归测试：全局中间件必须能通过
// context 读到路由模板。
//
// 背景：全局中间件包在 mux 外层，net/http 只在分发到命中 handler 时才填充
// r.Pattern，因此中间件里读 r.Pattern 恒为空。需要"按路由聚合"的中间件
// （熔断/指标）会退化成按具体路径聚合，导致统计割裂与内存无界增长。
// httpx.Server 现在会预判路由并把模板写入 context。
func TestServer_GlobalMiddlewareSeesRoutePattern(t *testing.T) {
	s := newTestServer()

	var patterns []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			patterns = append(patterns, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/users/{id}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})

	for _, p := range []string{"/users/1", "/users/2", "/users/999"} {
		rec := doRequest(t, s, http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, rec.Code, "path %s", p)
	}

	require.Len(t, patterns, 3)
	for _, got := range patterns {
		assert.Equal(t, "GET /users/{id}", got,
			"global middleware must see the route template, not the concrete path")
	}
}

// TestServer_GlobalMiddlewarePatternEmptyForUnmatched 验证未匹配路由时模板为空
// （不应把 404 请求归到某个路由上）。
func TestServer_GlobalMiddlewarePatternEmptyForUnmatched(t *testing.T) {
	s := newTestServer()

	var seen []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.AddRoute(Route{Method: "GET", Path: "/ok", Handler: okHandler()})

	rec := doRequest(t, s, http.MethodGet, "/missing", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Len(t, seen, 1)
	assert.Empty(t, seen[0], "unmatched requests must not resolve to a route pattern")
}

// TestServer_GlobalMiddlewarePatternWithNotFoundHandler 验证自定义 404 处理器
// 与模板注入共存时行为正确。
func TestServer_GlobalMiddlewarePatternWithNotFoundHandler(t *testing.T) {
	s := newTestServer()

	var seen []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		WriteHTTPError(w, http.StatusNotFound, "custom not found")
	})
	s.AddRoute(Route{Method: "GET", Path: "/users/{id}", Handler: okHandler()})

	// 命中路由
	rec := doRequest(t, s, http.MethodGet, "/users/1", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// 未命中：走自定义 404
	rec = doRequest(t, s, http.MethodGet, "/nope", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "custom not found")

	require.Len(t, seen, 2)
	assert.Equal(t, "GET /users/{id}", seen[0])
	assert.Empty(t, seen[1])
}
