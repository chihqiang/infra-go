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
	"sync"
	"syscall"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/logger"
	mp "github.com/chihqiang/infra-go/mapping"
)

// 本文件承载 HTTP 服务器 Server 核心：
//   - 核心类型（Middleware / Route / ServerConfig / Server）
//   - 服务器构造与路由注册（NewServer / AddRoute(s) / Use / Routes / PrintRoutes）
//   - 全局中间件懒加载与自定义 404（Handler / SetNotFoundHandler）
//   - 服务器启动与优雅关闭（Start / Shutdown / Stop）
//   - 内部路径辅助（buildPattern / normalizePath / joinPath）
//
// 说明：路由组选项 / 中间件包装 / Server 选项（With* / Apply*）在 server_options.go；
// 路由组 Group 在 server_group.go。

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

// Route 表示一个 HTTP 路由，注册后由 Server 分发。
type Route struct {
	// Method HTTP 方法（如 GET、POST），大小写不敏感。
	Method string
	// Path 路由路径，支持 Go 1.22 ServeMux 模式：/users/{id}、/files/{path...}。
	Path string
	// Handler 处理该路由的 HTTP 处理器。
	Handler http.HandlerFunc
}

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

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
// 使用 mapping.FillAndOverride 统一处理，零值视为"未设置"保留默认值。
// 要显式设为 0 请用对应的 RunOption，如 WithReadTimeout(0)。
func fillDefault(cfg ServerConfig) ServerConfig {
	var c ServerConfig
	mp.MustFillAndOverride(&c, cfg)
	return c
}

// --- 内部类型 ---

// Server 是一个 HTTP 服务器，支持路由注册、中间件和优雅关闭。
//
// 底层使用 http.ServeMux，原生支持：
//   - 方法匹配（GET / POST / PUT ...），自动返回 405 Method Not Allowed
//   - 路径参数（/users/{id}），通过 r.PathValue("id") 获取
//   - 通配路径（/files/{path...}），通过 r.PathValue("path") 获取
//   - 自动 404 Not Found
type Server struct {
	conf      ServerConfig
	mux       *http.ServeMux
	gmw       []Middleware
	gh        http.Handler // 缓存应用全局中间件后的根 handler，nil 表示需要重建
	handlerLk sync.Mutex   // 保护 gh 懒加载与重建（Use / SetNotFoundHandler 并发安全）
	tlsConfig *tls.Config

	// stateLk 保护以下可变状态：
	//   - routes：AddRoutes 写入，Routes/PrintRoutes 读取
	//   - httpServer：Start 写入，Shutdown/Stop 读取
	//
	// 二者都可能被多个 goroutine 并发访问（例如 service.ServiceGroup 在一个
	// goroutine 中启动、另一个中停止），无保护会构成数据竞争。
	stateLk    sync.RWMutex
	routes     []Route
	httpServer *http.Server

	// notFoundHandler 自定义 404 响应处理器（可选），通过 SetNotFoundHandler 设置。
	notFoundHandler http.HandlerFunc
}

// routesSnapshot 返回路由列表副本（调用方不得假设其后续不变）。
func (s *Server) routesSnapshot() []Route {
	s.stateLk.RLock()
	defer s.stateLk.RUnlock()
	out := make([]Route, len(s.routes))
	copy(out, s.routes)
	return out
}

// currentHTTPServer 返回当前正在运行的 http.Server；未启动时返回 nil。
// 用于 Shutdown/Stop 以避免与 Start 的写入竞争。
func (s *Server) currentHTTPServer() *http.Server {
	s.stateLk.RLock()
	defer s.stateLk.RUnlock()
	return s.httpServer
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

		s.stateLk.Lock()
		s.routes = append(s.routes, Route{
			Method:  strings.ToUpper(r.Method),
			Path:    r.Path,
			Handler: r.Handler, // 存原始 handler，便于 PrintRoutes 反射获取函数名
		})
		s.stateLk.Unlock()
	}
}

// Use 添加全局中间件，对所有已注册和后续注册的路由生效。
// 多个中间件按添加顺序执行（先添加的先执行）。
//
// 全局中间件在请求时动态应用（包装整个路由器），因此即使先注册路由、
// 再调用 Use，已注册的路由也会经过新添加的全局中间件。
// 注意：Start 启动后再调用 Use 不会影响已经运行的 httpServer。
func (s *Server) Use(mws ...Middleware) {
	s.handlerLk.Lock()
	s.gmw = append(s.gmw, mws...)
	s.gh = nil // 清空缓存，下次 Handler() 重新构建
	s.handlerLk.Unlock()
}

// Routes 返回已注册的所有路由（未应用中间件的原始 handler）。
// 返回的是副本，修改不会影响 Server 内部状态；并发安全。
func (s *Server) Routes() []Route {
	return s.routesSnapshot()
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
	routes := s.routesSnapshot()
	if len(routes) == 0 {
		fmt.Println("no routes registered")
		return
	}

	type routeEntry struct{ method, path, handler string }
	entries := make([]routeEntry, 0, len(routes))
	for _, r := range routes {
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
// 并发安全：懒加载构建受内部锁保护。
func (s *Server) Handler() http.Handler {
	s.handlerLk.Lock()
	defer s.handlerLk.Unlock()
	if s.gh == nil {
		s.buildGlobalHandler()
	}
	return s.gh
}

// buildGlobalHandler 构建应用了全局中间件、自定义 404 的根 handler（懒加载）。
// 全局中间件按添加顺序执行（先添加的先执行），包装整个 mux。
// 组中间件已在路由注册时烧录到各路由 handler，执行顺序为：
// 全局中间件 → 组中间件 → 路由 handler。
//
// handler 链（从内到外）：
//
//	mux（含自定义 404 判定）→ 全局中间件 → 错误渲染/路由模板注入
func (s *Server) buildGlobalHandler() {
	handler := http.HandlerFunc(s.mux.ServeHTTP)
	// 自定义 404 紧贴 mux，只对真正未匹配的路由生效。
	// 通过路由预判而非包装 ResponseWriter 判定"未匹配"，原因是后者无法区分
	// "路由未命中"与"业务主动返回 404"，会把业务 404 一并劫持；
	// 且包装 writer 会丢失 Flush/Hijack/Push/Unwrap 等可选能力，
	// 导致 SSE、WebSocket、HTTP/2 Push 静默失效。
	if s.notFoundHandler != nil {
		mux, notFound := s.mux, s.notFoundHandler
		handler = func(w http.ResponseWriter, r *http.Request) {
			if isMuxNotFound(mux, r) {
				notFound(w, r)
				return
			}
			mux.ServeHTTP(w, r)
		}
	}

	// 全局中间件（逆序包装，使先添加的中间件先执行）
	for i := len(s.gmw) - 1; i >= 0; i-- {
		handler = s.gmw[i](handler)
	}

	// 路由模板注入必须包在**中间件链最外层**：
	// 全局中间件位于 mux 外层，此时 net/http 还没有把匹配到的路由模板写入
	// r.Pattern（ServeMux 只在分发到命中 handler 时才填充）。
	// 需要"按路由聚合"的中间件（熔断、指标）因此拿不到稳定模板。
	// 这里先做一次路由预判、把模板放进 context，再进入中间件链，
	// 供 middleware.PatternFromContext 读取。
	//
	// 顺序很关键：若包在内层，中间件执行时 context 里还没有模板。
	mux := s.mux
	inner := handler
	// 仅在存在全局中间件/自定义 404 时才做路由预判（它们才需要模板）。
	needPattern := len(s.gmw) > 0 || s.notFoundHandler != nil
	s.gh = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按请求注入错误渲染：经 httpx 分发的请求保持统一 JSON 响应，
		// 而同进程内 gin/echo 路由不受影响（详见 middlewareErrorHandler）。
		ctx := middleware.ContextWithErrorHandler(r.Context(), middlewareErrorHandler)
		if needPattern {
			if pattern := matchedPattern(mux, r); pattern != "" {
				ctx = middleware.ContextWithPattern(ctx, pattern)
			}
		}
		inner(w, r.WithContext(ctx))
	})
}

// matchedPattern 返回该请求将被 mux 分发到的路由模板（如 "GET /users/{id}"）。
// 未匹配（含 405）时返回空字符串。
//
// 注意：只用 mux.Handler 查询，不调用 mux.ServeHTTP，
// 因此不会影响 ServeMux 后续对 r.Pattern 的填充。
func matchedPattern(mux *http.ServeMux, r *http.Request) string {
	_, pattern := mux.Handler(r)
	return pattern
}

// notFoundHandlerPtr 是 net/http 内置 404 处理器（http.NotFoundHandler()）的代码指针。
var notFoundHandlerPtr = reflect.ValueOf(http.NotFoundHandler()).Pointer()

// isMuxNotFound 判断该请求是否会被 mux 交给内置的 404 处理器。
//
// 不能只看 ServeMux.Handler 返回的 pattern：Go 1.22+ 在「无匹配」与
// 「路径匹配但方法不允许(405)」两种情况下都返回空 pattern，仅凭 pattern
// 判定会把 405 误判为 404（丢失 Allow 响应头）。因此需要同时满足：
// 空 pattern 且返回的 handler 正是内置 NotFoundHandler。
func isMuxNotFound(mux *http.ServeMux, r *http.Request) bool {
	h, pattern := mux.Handler(r)
	if pattern != "" {
		return false
	}
	v := reflect.ValueOf(h)
	// 命中的可能是任意实现了 http.Handler 的类型（非函数），此时不可能是内置 404。
	if v.Kind() != reflect.Func {
		return false
	}
	return v.Pointer() == notFoundHandlerPtr
}

// --- 自定义错误响应 ---

// SetNotFoundHandler 设置路由未找到（404）时的自定义响应处理器。
// 所有未被任何路由匹配的请求都会交给该处理器，替代默认的 "404 page not found"。
// 注意：处理器需自行写出状态码；若未调用 WriteHeader，将由 net/http 隐式写 200
// （httpx.OkJSON 系列即为此约定：HTTP 200 + 响应体中的业务错误码）。
// 业务路由内部主动返回的 404 不会被劫持，仍会原样返回给客户端。
//
//	server.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
//	    httpx.OkJSON(w, httpx.NewCodeError(httpx.CodeNotFound, "resource not found"))
//	})
func (s *Server) SetNotFoundHandler(h http.HandlerFunc) {
	s.handlerLk.Lock()
	s.notFoundHandler = h
	s.gh = nil
	s.handlerLk.Unlock()
}

// --- 启动与关闭 ---

// Start 启动 HTTP 服务器，支持优雅关闭。
//
// 服务器在独立 goroutine 中运行，主 goroutine 阻塞等待信号。
// 收到 SIGINT（Ctrl+C）、SIGTERM 或 SIGHUP 时执行优雅关闭。
//
// 如果配置了 CertFile 和 KeyFile，则启动 HTTPS 服务。
//
// 并发语义：Start 会先登记 http.Server 再开始监听，登记过程受锁保护，
// 因此另一 goroutine 调用 Stop/Shutdown 不会与登记过程竞争；
// 但若 Stop 在 Start 之前完成，则本次 Stop 是空操作（彼时服务器尚未启动）。
func (s *Server) Start() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.conf.Host, s.conf.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: s.conf.ReadTimeout,
		ReadTimeout:       s.conf.ReadTimeout,
		WriteTimeout:      s.conf.WriteTimeout,
		IdleTimeout:       s.conf.IdleTimeout,
		MaxHeaderBytes:    s.conf.MaxHeaderBytes,
		TLSConfig:         s.tlsConfig,
	}

	// 登记后再启动监听：Stop/Shutdown 通过 currentHTTPServer() 读取，
	// 无锁写入会与之构成数据竞争（-race 可检出）。
	s.stateLk.Lock()
	s.httpServer = srv
	s.stateLk.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if s.conf.CertFile != "" && s.conf.KeyFile != "" {
			errCh <- srv.ListenAndServeTLS(s.conf.CertFile, s.conf.KeyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	// 监听 SIGINT、SIGTERM 和 SIGHUP，兼容 Kubernetes 环境信号
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	// 必须注销：否则本函数返回后 sigCh 仍被注册在 signal 包中，
	// 后续信号会被投递到无人接收的 channel（缓冲满后静默丢弃），
	// 且同一进程多次 Start 会累积注册。
	defer signal.Stop(sigCh)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server error: %w", err)
	case sig := <-sigCh:
		logger.Info("httpx: received signal, shutting down",
			logger.String("signal", sig.String()))
		return s.Shutdown()
	}
}

// Shutdown 优雅关闭服务器，等待活跃连接处理完毕。
// 超时时间由 WithShutdownTimeout 设置（默认 10 秒）。
// 关闭失败时记录日志但不静默吞掉错误。
//
// 若服务器尚未启动（Start 未被调用），返回 nil（空操作）。
// 并发安全：可在与 Start 不同的 goroutine 中调用。
func (s *Server) Shutdown() error {
	srv := s.currentHTTPServer()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.conf.ShutdownTimeout)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("httpx: graceful shutdown failed",
			logger.Err(err),
			logger.Duration("timeout", s.conf.ShutdownTimeout))
	}
	return err
}

// Stop 停止服务器并返回关闭错误，委托 Shutdown。
// 提供与 Shutdown 等价的“停止”命名入口，便于适配要求 Stop() error 的管理接口
// （如配合 service.AsService 纳入 ServiceGroup 管理）；直接调用方也可像 Shutdown 一样获取错误。
func (s *Server) Stop() error {
	return s.Shutdown()
}

// --- 内部辅助函数 ---

// buildPattern 构建 ServeMux 的路由模式（格式："METHOD /path"）。
// 自动规范化路径：确保非空路径以 "/" 开头，否则 http.ServeMux
// 会因非法 pattern（如 "GET users"）而 panic。
func buildPattern(method, path string) string {
	method = strings.ToUpper(method)
	path = normalizePath(path)
	if method == "" || method == "*" {
		return path
	}
	return method + " " + path
}

// normalizePath 规范化路由路径：
//   - 空路径视为根路径 "/"
//   - 非空路径确保以 "/" 开头（自动补前导斜杠）
func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// joinPath 连接前缀和路径，处理多余的斜杠。
// 若 p 以 "/" 结尾（子树匹配模式，如 /static/），保留结尾斜杠；裸 "/" 除外。
func joinPath(prefix, p string) string {
	if prefix == "" {
		return p
	}
	joined := path.Join(prefix, p)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if strings.HasSuffix(p, "/") && p != "/" && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
