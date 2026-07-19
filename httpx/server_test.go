package httpx

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 辅助函数 ---

// newTestServer 创建测试用 Server（不启动 HTTP 服务，仅注册路由）。
func newTestServer(opts ...RunOption) *Server {
	return NewServer(ServerConfig{Host: "0.0.0.0", Port: 0}, opts...)
}

// doRequest 向 Server 的 Handler 发送请求，返回响应。
func doRequest(t *testing.T, s *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// doRequestWithHeaders 发送带自定义 Header 的请求。
func doRequestWithHeaders(t *testing.T, s *Server, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// --- 基础路由测试 ---

func TestAddRoute(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/hello",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "hello") },
	})

	rec := doRequest(t, s, http.MethodGet, "/hello", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hello")
}

func TestAddRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/list", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "list") }},
		{Method: "POST", Path: "/create", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "created") }},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/list", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "list")

	rec2 := doRequest(t, s, http.MethodPost, "/create", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "created")
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/users",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	// POST 到 GET 路由应返回 405
	rec := doRequest(t, s, http.MethodPost, "/users", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestNotFound(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/users",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	rec := doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- WithPrefix 测试 ---

func TestWithPrefix(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") }},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "created") }},
	}, WithPrefix("/api/v1"))

	// 带前缀的路由应可访问
	rec1 := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "users")

	rec2 := doRequest(t, s, http.MethodPost, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// 不带前缀的路由应 404
	rec3 := doRequest(t, s, http.MethodGet, "/users", nil)
	assert.Equal(t, http.StatusNotFound, rec3.Code)
}

func TestWithPrefix_EmptyPrefix(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") }},
	}, WithPrefix(""))

	rec := doRequest(t, s, http.MethodGet, "/ping", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "pong")
}

func TestWithPrefix_NestedParams(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, r.PathValue("id"))
		}},
	}, WithPrefix("/api/v1"))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/users/42", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "42")
}

// --- 中间件测试 ---

// recordingMiddleware 记录中间件执行顺序。
func recordingMiddleware(name string, order *[]string) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name+"-before")
			next(w, r)
			*order = append(*order, name+"-after")
		}
	}
}

func TestMiddleware_WithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("mw1", &order)))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, []string{"mw1-before", "handler", "mw1-after"}, order)
}

func TestMiddleware_MultipleWithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddlewares(
		recordingMiddleware("mw1", &order),
		recordingMiddleware("mw2", &order),
	))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 执行顺序：mw1 → mw2 → handler
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

func TestMiddleware_GlobalUse(t *testing.T) {
	var order []string
	s := newTestServer()
	s.Use(recordingMiddleware("global", &order))
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("group", &order)))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 全局中间件 → 组中间件 → handler
	assert.Equal(t, []string{"global-before", "group-before", "handler", "group-after", "global-after"}, order)
}

func TestMiddleware_GlobalUseMultiple(t *testing.T) {
	var order []string
	s := newTestServer()
	s.Use(
		recordingMiddleware("g1", &order),
		recordingMiddleware("g2", &order),
	)
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddlewares(
		recordingMiddleware("grp1", &order),
		recordingMiddleware("grp2", &order),
	))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 全局中间件先执行 → 组中间件 → handler
	assert.Equal(t, []string{
		"g1-before", "g2-before",
		"grp1-before", "grp2-before",
		"handler",
		"grp2-after", "grp1-after",
		"g2-after", "g1-after",
	}, order)
}

func TestMiddleware_ShortCircuit(t *testing.T) {
	s := newTestServer()

	handlerCalled := false
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/blocked", Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		}},
	}, WithMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 不调用 next，直接返回
			WriteHTTPError(w, http.StatusForbidden, "blocked")
		}
	}))

	rec := doRequest(t, s, http.MethodGet, "/blocked", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, handlerCalled, "handler should not be called when middleware short-circuits")
}

// TestMiddleware_UseAfterAddRoute 验证 Use 添加的全局中间件对"已注册"的路由也生效。
// 这是修复核心 bug 的测试：之前 AddRoutes 在注册时把全局中间件烧录到 handler，
// 导致后续 Use 添加的中间件无法作用于已注册路由。
func TestMiddleware_UseAfterAddRoute(t *testing.T) {
	var order []string
	s := newTestServer()

	// 先注册路由
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		},
	})

	// 再添加全局中间件（文档承诺对已注册路由生效）
	s.Use(recordingMiddleware("global", &order))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 全局中间件应该作用于已注册的路由
	assert.Equal(t, []string{"global-before", "handler", "global-after"}, order)
}

// TestMiddleware_UseAfterAddRouteMultiple 验证多个全局中间件在路由注册后添加也能按顺序生效。
func TestMiddleware_UseAfterAddRouteMultiple(t *testing.T) {
	var order []string
	s := newTestServer()

	// 先注册路由
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		},
	})

	// 再添加多个全局中间件
	s.Use(
		recordingMiddleware("g1", &order),
		recordingMiddleware("g2", &order),
	)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// g1 → g2 → handler
	assert.Equal(t, []string{"g1-before", "g2-before", "handler", "g2-after", "g1-after"}, order)
}

// TestMiddleware_UseBeforeAndAfterAddRoute 验证 Use 在路由注册前后都调用时，
// 全局中间件和组中间件都能正确作用于路由，且执行顺序正确。
func TestMiddleware_UseBeforeAndAfterAddRoute(t *testing.T) {
	var order []string
	s := newTestServer()

	// 先添加一个全局中间件
	s.Use(recordingMiddleware("g1", &order))

	// 注册路由（带组中间件）
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("grp", &order)))

	// 再添加一个全局中间件
	s.Use(recordingMiddleware("g2", &order))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// g1 → g2 → grp → handler
	assert.Equal(t, []string{
		"g1-before", "g2-before",
		"grp-before",
		"handler",
		"grp-after",
		"g2-after", "g1-after",
	}, order)
}

// TestMiddleware_GlobalOnNotFound 验证全局中间件也会作用于 404 请求。
// 因为全局中间件包装了整个 mux，404 handler 也会经过全局中间件。
func TestMiddleware_GlobalOnNotFound(t *testing.T) {
	var order []string
	s := newTestServer()

	s.Use(recordingMiddleware("global", &order))
	// 不注册任何路由，请求会 404

	rec := doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// 全局中间件 before/after 都应执行
	assert.Equal(t, []string{"global-before", "global-after"}, order)
}

// TestMiddleware_GlobalShortCircuit 验证全局中间件短路时不会到达路由 handler。
func TestMiddleware_GlobalShortCircuit(t *testing.T) {
	handlerCalled := false
	s := newTestServer()

	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			OkJSON(w, "ok")
		},
	})

	// 全局中间件短路：不调用 next
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			WriteHTTPError(w, http.StatusUnauthorized, "unauthorized")
		}
	})

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, handlerCalled, "handler should not be called when global middleware short-circuits")
}

// --- ApplyMiddleware 独立函数测试 ---

func TestApplyMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()

	wrapped := ApplyMiddleware(recordingMiddleware("mw", &order),
		Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)
	s.AddRoutes(wrapped)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw-before", "handler", "mw-after"}, order)
}

func TestApplyMiddlewares(t *testing.T) {
	var order []string
	s := newTestServer()

	mws := []Middleware{
		recordingMiddleware("mw1", &order),
		recordingMiddleware("mw2", &order),
	}
	wrapped := ApplyMiddlewares(mws,
		Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)
	s.AddRoutes(wrapped)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

// --- Group 测试 ---

func TestGroup(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api")
	api.AddRoute(Route{
		Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") },
	})

	rec := doRequest(t, s, http.MethodGet, "/api/ping", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "pong")
}

func TestGroup_WithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api", recordingMiddleware("mw1", &order))
	api.Use(recordingMiddleware("mw2", &order))
	api.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/api/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

func TestGroup_Nested(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api", recordingMiddleware("api-mw", &order))
	v1 := api.Group("/v1", recordingMiddleware("v1-mw", &order))
	v1.AddRoute(Route{
		Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "users")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "users")

	// api-mw → v1-mw → handler
	assert.Equal(t, []string{"api-mw-before", "v1-mw-before", "handler", "v1-mw-after", "api-mw-after"}, order)
}

func TestGroup_AddRoutes(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api/v1")
	api.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "list") }},
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, r.PathValue("id")) }},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "list")

	rec2 := doRequest(t, s, http.MethodGet, "/api/v1/users/99", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "99")
}

// TestGroup_AddRouteWithOptions 验证 Group.AddRoute 现在也接受 RouteOption，
// 与 Server.AddRoute 的 API 保持一致。
func TestGroup_AddRouteWithOptions(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api/v1")
	// Group.AddRoute 附加额外的中间件（RouteOption）
	api.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	}, WithMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-MW", "applied")
			next(w, r)
		}
	}))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
	assert.Equal(t, "applied", rec.Header().Get("X-MW"))
}

// TestGroup_AddRoutesWithOptions 验证 Group.AddRoutes 也接受 RouteOption。
func TestGroup_AddRoutesWithOptions(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api/v1")
	api.AddRoutes([]Route{
		{Method: "GET", Path: "/a", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler-a")
		}},
		{Method: "GET", Path: "/b", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler-b")
		}},
	}, WithMiddleware(recordingMiddleware("extra", &order)))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/a", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"extra-before", "handler-a", "extra-after"}, order)
}

// --- 路径参数测试 ---

func TestWildcardPath(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method: "GET",
		Path:   "/files/{path...}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			p := r.PathValue("path")
			OkJSON(w, p)
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/files/dir/sub/file.txt", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "dir/sub/file.txt")
}

// --- Routes / PrintRoutes 测试 ---

func TestRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
	}, WithPrefix("/api"))

	routes := s.Routes()
	assert.Len(t, routes, 2)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/api/users", routes[0].Path)
	assert.Equal(t, "POST", routes[1].Method)
	assert.Equal(t, "/api/users", routes[1].Path)

	// 验证返回的是副本
	routes[0].Method = "DELETE"
	original := s.Routes()
	assert.Equal(t, "GET", original[0].Method)
}

func TestPrintRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
	}, WithPrefix("/api"))

	// 不 panic 即可
	s.PrintRoutes()
}

func TestPrintRoutes_Empty(t *testing.T) {
	s := newTestServer()
	// 不 panic 即可
	s.PrintRoutes()
}

// --- 辅助函数测试 ---

func TestBuildPattern(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/users", "GET /users"},
		{"get", "/users", "GET /users"},
		{"POST", "/users/create", "POST /users/create"},
		{"", "/health", "/health"},
		{"*", "/health", "/health"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, buildPattern(tt.method, tt.path))
		})
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		prefix string
		path   string
		want   string
	}{
		{"", "/users", "/users"},
		{"/api", "/users", "/api/users"},
		{"/api/", "/users", "/api/users"},
		{"/api", "users", "/api/users"},
		{"/api/v1", "/users/{id}", "/api/v1/users/{id}"},
		{"/api", "/", "/api"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix+"+"+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, joinPath(tt.prefix, tt.path))
		})
	}
}

// --- Server 配置测试 ---

func TestNewServer_DefaultHost(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080})
	assert.Equal(t, "0.0.0.0", s.conf.Host)
	assert.Equal(t, 8080, s.conf.Port)
}

func TestNewServer_CustomHost(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 9090})
	assert.Equal(t, "127.0.0.1", s.conf.Host)
	assert.Equal(t, 9090, s.conf.Port)
}

func TestNewServer_DefaultTimeouts(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080})
	assert.Equal(t, 10*time.Second, s.conf.ReadTimeout)
	assert.Equal(t, 10*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 1048576, s.conf.MaxHeaderBytes)
	assert.Equal(t, 10*time.Second, s.conf.ShutdownTimeout)
}

func TestNewServer_WithOptions(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080},
		WithReadTimeout(30*time.Second),
		WithWriteTimeout(60*time.Second),
		WithIdleTimeout(120*time.Second),
		WithMaxHeaderBytes(1<<20),
		WithShutdownTimeout(5*time.Second),
	)
	assert.Equal(t, 30*time.Second, s.conf.ReadTimeout)
	assert.Equal(t, 60*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 1<<20, s.conf.MaxHeaderBytes)
	assert.Equal(t, 5*time.Second, s.conf.ShutdownTimeout)
}

// TestNewServer_ZeroTimeoutViaRunOption 验证通过 RunOption 可以把 timeout 显式设为 0（不限制），
// 不会被 fillDefault 的默认值覆盖。
func TestNewServer_ZeroTimeoutViaRunOption(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080},
		WithReadTimeout(0),
		WithWriteTimeout(0),
		WithIdleTimeout(0),
		WithShutdownTimeout(0),
	)
	assert.Equal(t, time.Duration(0), s.conf.ReadTimeout, "WithReadTimeout(0) 应生效，不被默认值覆盖")
	assert.Equal(t, time.Duration(0), s.conf.WriteTimeout)
	assert.Equal(t, time.Duration(0), s.conf.IdleTimeout)
	assert.Equal(t, time.Duration(0), s.conf.ShutdownTimeout)
}

// TestNewServer_ZeroTimeoutViaConfig 验证通过 ServerConfig 设 0 会被当作"未设置"而使用默认值。
func TestNewServer_ZeroTimeoutViaConfig(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080}) // 所有 timeout 为零值
	assert.Equal(t, 10*time.Second, s.conf.ReadTimeout, "ServerConfig 零值应使用默认值")
	assert.Equal(t, 10*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 10*time.Second, s.conf.ShutdownTimeout)
}

// --- 集成测试 ---

func TestServer_Integration(t *testing.T) {
	var mu sync.Mutex
	requests := make(map[string]string)

	s := newTestServer()

	// 全局日志中间件
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r)
		}
	})

	// 公开路由
	s.AddRoute(Route{
		Method: "GET", Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") },
	})

	// API v1 路由组（带前缀和中间件）
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests["list"] = "called"
			mu.Unlock()
			OkJSON(w, "list")
		}},
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, r.PathValue("id"))
		}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "created")
		}},
	}, WithPrefix("/api/v1"))

	// 使用 Group 创建子路由组
	admin := s.Group("/admin")
	admin.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				WriteHTTPError(w, http.StatusForbidden, "admin required")
				return
			}
			next(w, r)
		}
	})
	admin.AddRoute(Route{
		Method: "DELETE", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "deleted")
		}},
	)

	// 测试公开路由
	rec := doRequest(t, s, http.MethodGet, "/health", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")

	// 测试 API v1 路由
	rec = doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "list")

	// 测试路径参数
	rec = doRequest(t, s, http.MethodGet, "/api/v1/users/42", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "42")

	// 测试 POST
	rec = doRequest(t, s, http.MethodPost, "/api/v1/users", strings.NewReader(`{"name":"test"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "created")

	// 测试 admin 路由（无 header 应被拦截）
	rec = doRequest(t, s, http.MethodDelete, "/admin/users/1", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// 测试 admin 路由（有 header 应通过）
	rec = doRequestWithHeaders(t, s, http.MethodDelete, "/admin/users/1", nil, map[string]string{"X-Admin": "true"})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "deleted")

	// 打印路由
	s.PrintRoutes()

	// 验证路由数量
	routes := s.Routes()
	assert.Len(t, routes, 5)
}

// --- Start/Shutdown 测试 ---

func TestServer_StartAndShutdown(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0, ShutdownTimeout: 2 * time.Second})
	s.AddRoute(Route{
		Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") },
	})

	// 找一个可用端口
	ln, err := newTestListener()
	require.NoError(t, err)
	port := ln.Addr().(*testAddr).port
	ln.Close()

	s.conf.Port = port

	// 启动服务器
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	// 等待服务器就绪
	var lastErr error
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", port))
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		lastErr = nil
		break
	}
	require.NoError(t, lastErr, "server should be ready")

	// 关闭服务器
	err = s.Shutdown()
	require.NoError(t, err)

	// 验证服务器已关闭
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

func TestServer_ShutdownNotStarted(t *testing.T) {
	s := newTestServer()
	err := s.Shutdown()
	assert.NoError(t, err)
}

func TestServer_ContextPropagation(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method: "GET", Path: "/ctx", Handler: func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			_ = ctx // context 应可用于取消、超时等
			OkJSON(w, "ok")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/ctx", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- net.Listener 辅助 ---

// testAddr 用于获取测试端口。
type testAddr struct {
	network string
	port    int
}

func (a *testAddr) Network() string { return a.network }
func (a *testAddr) String() string  { return fmt.Sprintf("127.0.0.1:%d", a.port) }

// testListener 用于获取可用端口。
type testListener struct {
	addr *testAddr
}

func (l *testListener) Accept() (net.Conn, error) { return nil, nil }
func (l *testListener) Close() error              { return nil }
func (l *testListener) Addr() net.Addr            { return l.addr }

func newTestListener() (*testListener, error) {
	// 使用 net.Listen 获取一个可用端口，然后关闭
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close()
	return &testListener{addr: &testAddr{network: "tcp", port: addr.Port}}, nil
}
