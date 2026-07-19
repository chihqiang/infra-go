package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	mp "github.com/chihqiang/infra-go/mapping"
)

// --- 核心类型 ---

// Middleware 是 HTTP 中间件函数。
// 接收下游 handler，返回包装后的 handler。
//
// 约定：中间件调用 next(w, r) 将请求传递给下游，不调用则中断链路。
//
//	func Logging(next http.HandlerFunc) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        start := time.Now()
//	        next(w, r)
//	        log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
//	    }
//	}
type Middleware func(http.HandlerFunc) http.HandlerFunc

// Route 表示一个 HTTP 路由。
type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

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

// ServerConfig 是 HTTP 服务器配置。
// 使用 json 标签声明默认值和约束，兼容 conf 包从配置文件加载。
type ServerConfig struct {
	// Host 监听地址，默认 "0.0.0.0"。
	Host string `json:",default=0.0.0.0"`
	// Port 监听端口，默认 8080。
	Port int `json:",default=8080,range=[1:65535]"`
	// CertFile TLS 证书文件路径（可选，设置后启用 HTTPS）。
	CertFile string `json:",optional"`
	// KeyFile TLS 私钥文件路径（可选）。
	KeyFile string `json:",optional"`
	// ReadTimeout 读超时，默认 10s。
	// 通过 ServerConfig 设 0 会被当作“未设置”而使用默认值；
	// 若需设为 0（不限制），请用 WithReadTimeout(0)。
	ReadTimeout time.Duration `json:",default=10s"`
	// WriteTimeout 写超时，默认 10s。
	// 通过 ServerConfig 设 0 会被当作“未设置”而使用默认值；
	// 若需设为 0（不限制），请用 WithWriteTimeout(0)。
	WriteTimeout time.Duration `json:",default=10s"`
	// IdleTimeout 空闲连接超时，默认 120s。
	// 通过 ServerConfig 设 0 会被当作“未设置”而使用默认值；
	// 若需设为 0（不限制），请用 WithIdleTimeout(0)。
	IdleTimeout time.Duration `json:",default=120s"`
	// MaxHeaderBytes 最大请求头字节数，默认 1MB。
	MaxHeaderBytes int `json:",default=1048576"`
	// ShutdownTimeout 优雅关闭超时时间，默认 10s。
	// 通过 ServerConfig 设 0 会被当作“未设置”而使用默认值；
	// 若需设为 0，请用 WithShutdownTimeout(0)。
	ShutdownTimeout time.Duration `json:",default=10s"`
}

// fillDefaultUnmarshaler 用于填充 ServerConfig 默认值的反序列化器。
var fillDefaultUnmarshaler = mp.NewUnmarshaler("json", mp.WithDefault())

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
func fillDefault(cfg ServerConfig) ServerConfig {
	var c ServerConfig
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	// 用用户配置覆盖（零值视为“未设置”，保留默认值；
	// 要显式设为 0 请用对应的 RunOption，如 WithReadTimeout(0)）
	if cfg.Host != "" {
		c.Host = cfg.Host
	}
	if cfg.Port != 0 {
		c.Port = cfg.Port
	}
	if cfg.CertFile != "" {
		c.CertFile = cfg.CertFile
	}
	if cfg.KeyFile != "" {
		c.KeyFile = cfg.KeyFile
	}
	if cfg.ReadTimeout != 0 {
		c.ReadTimeout = cfg.ReadTimeout
	}
	if cfg.WriteTimeout != 0 {
		c.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.IdleTimeout != 0 {
		c.IdleTimeout = cfg.IdleTimeout
	}
	if cfg.MaxHeaderBytes != 0 {
		c.MaxHeaderBytes = cfg.MaxHeaderBytes
	}
	if cfg.ShutdownTimeout != 0 {
		c.ShutdownTimeout = cfg.ShutdownTimeout
	}

	return c
}

// --- 内部类型 ---

// routeGroup 是一组路由及其配置。
type routeGroup struct {
	routes      []Route
	middlewares []Middleware
}

// Server 是一个 HTTP 服务器，支持路由注册、中间件和优雅关闭。
//
// 底层使用 http.ServeMux，原生支持：
//   - 方法匹配（GET / POST / PUT ...），自动返回 405 Method Not Allowed
//   - 路径参数（/users/{id}），通过 r.PathValue("id") 获取
//   - 通配路径（/files/{path...}），通过 r.PathValue("path") 获取
//   - 自动 404 Not Found
type Server struct {
	conf       ServerConfig
	mux        *http.ServeMux
	gmw        []Middleware
	gh         http.Handler // 缓存应用全局中间件后的根 handler，nil 表示需要重建
	routes     []Route
	httpServer *http.Server
	tlsConfig  *tls.Config
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

// --- Server 构造与路由注册 ---

// NewServer 创建一个 HTTP 服务器。
//
//	conf := httpx.ServerConfig{
//	    Host: "0.0.0.0",
//	    Port: 8080,
//	}
//	server := httpx.NewServer(conf, httpx.WithReadTimeout(30*time.Second))
func NewServer(conf ServerConfig, opts ...RunOption) *Server {
	conf = fillDefault(conf)
	s := &Server{
		conf: conf,
		mux:  http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// AddRoute 添加单个路由，可附加 RouteOption。
func (s *Server) AddRoute(r Route, opts ...RouteOption) {
	s.AddRoutes([]Route{r}, opts...)
}

// AddRoutes 添加一组路由。
//
// opts 中的 RouteOption 会统一应用到这组路由（如前缀、中间件）。
//
// 中间件执行顺序：全局中间件（Use 添加）→ 组中间件（WithMiddleware 添加）→ 路由 handler。
func (s *Server) AddRoutes(rs []Route, opts ...RouteOption) {
	g := routeGroup{routes: rs}
	for _, opt := range opts {
		opt(&g)
	}

	for _, r := range g.routes {
		handler := r.Handler

		// 应用组中间件（逆序包装，使先添加的中间件先执行）
		for i := len(g.middlewares) - 1; i >= 0; i-- {
			handler = g.middlewares[i](handler)
		}
		// 全局中间件不在注册时烧录，而是在请求时由 gh 动态应用，
		// 这样 Use 添加的全局中间件可以对已注册的路由也生效。

		pattern := buildPattern(r.Method, r.Path)
		s.mux.HandleFunc(pattern, handler)
		s.routes = append(s.routes, Route{
			Method:  strings.ToUpper(r.Method),
			Path:    r.Path,
			Handler: r.Handler, // 存原始 handler，便于 PrintRoutes 反射获取函数名
		})
	}
}

// Use 添加全局中间件，对所有已注册和后续注册的路由生效。
// 多个中间件按添加顺序执行（先添加的先执行）。
//
// 全局中间件在请求时动态应用（包装整个路由器），因此即使先注册路由、
// 再调用 Use，已注册的路由也会经过新添加的全局中间件。
// 注意：Start 启动后再调用 Use 不会影响已经运行的 httpServer。
func (s *Server) Use(mws ...Middleware) {
	s.gmw = append(s.gmw, mws...)
	s.gh = nil // 清空缓存，下次 Handler() 重新构建
}

// Routes 返回已注册的所有路由（已应用中间件）。
// 返回的是副本，修改不会影响 Server 内部状态。
func (s *Server) Routes() []Route {
	routes := make([]Route, len(s.routes))
	copy(routes, s.routes)
	return routes
}

// PrintRoutes 打印已注册的路由列表。
//
//	server.PrintRoutes()
//	// 输出：
//	// DELETE  /admin/users/{id}   --> main.deleteUser
//	// GET     /api/v1/users       --> main.listUsers
//	// GET     /api/v1/users/{id}   --> main.getUser
//	// GET     /health             --> main.health
//	// POST    /api/v1/users       --> main.createUser
//	//
//	// 5 routes registered
func (s *Server) PrintRoutes() {
	if len(s.routes) == 0 {
		fmt.Println("no routes registered")
		return
	}

	type routeEntry struct{ method, path, handler string }
	entries := make([]routeEntry, 0, len(s.routes))
	for _, r := range s.routes {
		entries = append(entries, routeEntry{
			method:  r.Method,
			path:    r.Path,
			handler: handlerName(r.Handler),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path != entries[j].path {
			return entries[i].path < entries[j].path
		}
		return entries[i].method < entries[j].method
	})

	// 计算列宽
	mw, pw := 0, 0
	for _, e := range entries {
		if len(e.method) > mw {
			mw = len(e.method)
		}
		if len(e.path) > pw {
			pw = len(e.path)
		}
	}

	for _, e := range entries {
		fmt.Printf("%-*s  %-*s  --> %s\n", mw, e.method, pw, e.path, e.handler)
	}
	fmt.Printf("\n%d routes registered\n", len(entries))
}

// handlerName 通过反射获取 http.HandlerFunc 的函数名（含包路径）。
// 获取不到时返回空字符串。
func handlerName(h http.HandlerFunc) string {
	if h == nil {
		return ""
	}
	return runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
}

// Mux 返回底层的 ServeMux，用于高级场景（如手动注册路由）。
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Handler 返回服务器的 HTTP Handler，可用于 httptest 等场景。
// 返回的 handler 已应用全局中间件（Use 添加）。
func (s *Server) Handler() http.Handler {
	if s.gh == nil {
		s.buildGlobalHandler()
	}
	return s.gh
}

// buildGlobalHandler 构建应用了全局中间件的根 handler（懒加载）。
// 全局中间件按添加顺序执行（先添加的先执行），包装整个 mux。
// 组中间件已在路由注册时烧录到各路由 handler，执行顺序为：
// 全局中间件 → 组中间件 → 路由 handler。
func (s *Server) buildGlobalHandler() {
	if len(s.gmw) == 0 {
		s.gh = s.mux
		return
	}
	// 用全局中间件包装整个 mux（逆序包装，使先添加的中间件先执行）
	handler := http.HandlerFunc(s.mux.ServeHTTP)
	for i := len(s.gmw) - 1; i >= 0; i-- {
		handler = s.gmw[i](handler)
	}
	s.gh = handler
}

// --- 路由组（Group）---

// Group 是一个路由组，共享路径前缀和中间件。
// 支持链式调用和嵌套，便于按模块组织路由。
type Group struct {
	server      *Server
	prefix      string
	middlewares []Middleware
}

// Group 创建一个路由组。
//
//	api := server.Group("/api/v1")
//	api.AddRoute(httpx.Route{Method: "GET", Path: "/users", Handler: listUsers})
func (s *Server) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		server:      s,
		prefix:      prefix,
		middlewares: append([]Middleware(nil), mws...),
	}
}

// Use 添加中间件到路由组。
func (g *Group) Use(mws ...Middleware) {
	g.middlewares = append(g.middlewares, mws...)
}

// AddRoute 添加单个路由到路由组，可附加 RouteOption。
// opts 中的 RouteOption 会追加在组前缀、组中间件之后应用。
func (g *Group) AddRoute(r Route, opts ...RouteOption) {
	g.AddRoutes([]Route{r}, opts...)
}

// AddRoutes 添加多个路由到路由组，可附加 RouteOption。
// 路由路径会自动拼接组前缀，handler 会应用组中间件。
// opts 中的 RouteOption 会追加在组前缀、组中间件之后应用。
func (g *Group) AddRoutes(rs []Route, opts ...RouteOption) {
	allOpts := []RouteOption{WithPrefix(g.prefix)}
	if len(g.middlewares) > 0 {
		allOpts = append(allOpts, WithMiddlewares(g.middlewares...))
	}
	allOpts = append(allOpts, opts...)
	g.server.AddRoutes(rs, allOpts...)
}

// Group 创建子路由组，继承父组的前缀和中间件。
//
//	api := server.Group("/api", logMiddleware)
//	v1 := api.Group("/v1", authMiddleware)
//	// 路由前缀 /api/v1，中间件 logMiddleware → authMiddleware
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		server:      g.server,
		prefix:      joinPath(g.prefix, prefix),
		middlewares: append(append([]Middleware(nil), g.middlewares...), mws...),
	}
}

// --- 启动与关闭 ---

// Start 启动 HTTP 服务器，支持优雅关闭。
//
// 服务器在独立 goroutine 中运行，主 goroutine 阻塞等待信号。
// 收到 SIGINT（Ctrl+C）或 SIGTERM 时执行优雅关闭。
//
// 如果配置了 CertFile 和 KeyFile，则启动 HTTPS 服务。
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.conf.Host, s.conf.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: s.conf.ReadTimeout,
		ReadTimeout:       s.conf.ReadTimeout,
		WriteTimeout:      s.conf.WriteTimeout,
		IdleTimeout:       s.conf.IdleTimeout,
		MaxHeaderBytes:    s.conf.MaxHeaderBytes,
		TLSConfig:         s.tlsConfig,
	}

	errCh := make(chan error, 1)
	go func() {
		if s.conf.CertFile != "" && s.conf.KeyFile != "" {
			errCh <- s.httpServer.ListenAndServeTLS(s.conf.CertFile, s.conf.KeyFile)
		} else {
			errCh <- s.httpServer.ListenAndServe()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server error: %w", err)
	case <-sigCh:
		return s.Shutdown()
	}
}

// Shutdown 优雅关闭服务器，等待活跃连接处理完毕。
// 超时时间由 WithShutdownTimeout 设置（默认 10 秒）。
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.conf.ShutdownTimeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// --- 内部辅助函数 ---

// buildPattern 构建 ServeMux 的路由模式（格式："METHOD /path"）。
func buildPattern(method, path string) string {
	method = strings.ToUpper(method)
	if method == "" || method == "*" {
		return path
	}
	return method + " " + path
}

// joinPath 连接前缀和路径，处理多余的斜杠。
func joinPath(prefix, p string) string {
	if prefix == "" {
		return p
	}
	joined := path.Join(prefix, p)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return joined
}
