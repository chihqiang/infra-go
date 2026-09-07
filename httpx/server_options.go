package httpx

import (
	"crypto/tls"
	"net/http"
	"time"
)

// 本文件承载 Server / 路由组的各类选项（With*）与中间件包装（Apply*）：
//   - 选项类型（RouteOption / RunOption）与 RouteOption 的作用对象 routeGroup
//   - 路由组选项（WithPrefix / WithMiddleware / WithMiddlewares）
//   - 独立函数形式的中间件包装（ApplyMiddleware / ApplyMiddlewares）
//   - 标准中间件适配（AsMiddleware）
//   - Server 选项（WithReadTimeout / WithWriteTimeout / WithIdleTimeout /
//     WithMaxHeaderBytes / WithTLSConfig / WithShutdownTimeout）

// --- 选项类型 ---

// RouteOption 用于自定义一组路由的选项，如前缀、中间件。
type RouteOption func(*routeGroup)

// RunOption 用于自定义 Server 的选项，如超时、TLS。
// 也可直接传入闭包，在构造时注册路由、添加中间件等：
//
//	server := httpx.NewServer(conf, func(s *httpx.Server) {
//	    s.Use(loggingMiddleware)
//	    s.AddRoute(httpx.Route{Method: "GET", Path: "/ping", Handler: ping})
//	})
type RunOption func(*Server)

// routeGroup 是一组路由及其配置，是 RouteOption 的作用对象。
type routeGroup struct {
	routes      []Route
	middlewares []Middleware
}

// --- 路由组选项（RouteOption）---

// WithPrefix 为路由组添加路径前缀。
//
//	server.AddRoutes([]Route{
//	    {Method: "GET", Path: "/users", Handler: listUsers},
//	    {Method: "POST", Path: "/users", Handler: createUser},
//	}, httpx.WithPrefix("/api/v1"))
//
// 注册的路由为：GET /api/v1/users, POST /api/v1/users
func WithPrefix(prefix string) RouteOption {
	return func(g *routeGroup) {
		if prefix == "" {
			return
		}
		routes := make([]Route, 0, len(g.routes))
		for _, r := range g.routes {
			routes = append(routes, Route{
				Method:  r.Method,
				Path:    joinPath(prefix, r.Path),
				Handler: r.Handler,
			})
		}
		g.routes = routes
	}
}

// WithMiddleware 为路由组添加一个中间件。
// 中间件按添加顺序执行（先添加的先执行）。
func WithMiddleware(mw Middleware) RouteOption {
	return func(g *routeGroup) {
		g.middlewares = append(g.middlewares, mw)
	}
}

// WithMiddlewares 为路由组添加多个中间件。
// 中间件按传入顺序执行（第一个先执行）。
func WithMiddlewares(mws ...Middleware) RouteOption {
	return func(g *routeGroup) {
		g.middlewares = append(g.middlewares, mws...)
	}
}

// --- 独立函数形式的中间件包装 ---

// ApplyMiddleware 将中间件应用到路由，返回包装后的路由。
// 适用于需要在添加路由前对特定路由包装中间件的场景。
//
//	server.AddRoutes(httpx.ApplyMiddleware(authMiddleware,
//	    httpx.Route{Method: "GET", Path: "/profile", Handler: getProfile},
//	    httpx.Route{Method: "PUT", Path: "/profile", Handler: updateProfile},
//	))
func ApplyMiddleware(mw Middleware, rs ...Route) []Route {
	routes := make([]Route, len(rs))
	for i, r := range rs {
		routes[i] = Route{
			Method:  r.Method,
			Path:    r.Path,
			Handler: mw(r.Handler),
		}
	}
	return routes
}

// ApplyMiddlewares 将多个中间件应用到路由，返回包装后的路由。
// 中间件按切片顺序执行（第一个先执行）。
func ApplyMiddlewares(mws []Middleware, rs ...Route) []Route {
	for i := len(mws) - 1; i >= 0; i-- {
		rs = ApplyMiddleware(mws[i], rs...)
	}
	return rs
}

// --- 标准中间件适配 ---

// AsMiddleware 将标准形式的中间件适配为 httpx.Middleware，方便快速把
// 任意 func(http.Handler) http.Handler 中间件（如 httpx/middleware 子包的
// NewXxx().Middleware()，或其它基于 net/http 的第三方中间件）注册到 server。
//
//	// 自定义/第三方标准中间件 → httpx 中间件
//	server.Use(httpx.AsMiddleware(myStdMiddleware))
//
//	// 使用 httpx/middleware 子包（OO 形态）时亦可通过本函数接入：
//	server.Use(httpx.AsMiddleware(middleware.NewCORS("*").Middleware()))
func AsMiddleware(mw func(http.Handler) http.Handler) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return mw(http.HandlerFunc(next)).ServeHTTP
	}
}

// --- Server 选项（RunOption）---

// WithReadTimeout 设置读超时。覆盖配置中的 ReadTimeout。
func WithReadTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.ReadTimeout = d
	}
}

// WithWriteTimeout 设置写超时。覆盖配置中的 WriteTimeout。
func WithWriteTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.WriteTimeout = d
	}
}

// WithIdleTimeout 设置空闲连接超时。覆盖配置中的 IdleTimeout。
func WithIdleTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.IdleTimeout = d
	}
}

// WithMaxHeaderBytes 设置最大请求头字节数。覆盖配置中的 MaxHeaderBytes。
func WithMaxHeaderBytes(n int) RunOption {
	return func(s *Server) {
		s.conf.MaxHeaderBytes = n
	}
}

// WithTLSConfig 设置 TLS 配置。
func WithTLSConfig(cfg *tls.Config) RunOption {
	return func(s *Server) {
		s.tlsConfig = cfg
	}
}

// WithShutdownTimeout 设置优雅关闭超时时间，覆盖配置中的 ShutdownTimeout。
func WithShutdownTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.ShutdownTimeout = d
	}
}
