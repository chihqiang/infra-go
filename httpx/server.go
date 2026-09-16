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

// This file holds the core of the HTTP server, Server:
//   - Core types (Middleware / Route / ServerConfig / Server)
//   - Server construction and route registration
//     (NewServer / AddRoute(s) / Use / Routes / PrintRoutes)
//   - Lazy global middleware and custom 404 (Handler / SetNotFoundHandler)
//   - Server start and graceful shutdown (Start / Shutdown / Stop)
//   - Internal path helpers (buildPattern / normalizePath / joinPath)
//
// Note: route group options / middleware wrappers / Server options (With* / Apply*)
// live in server_options.go; the route group Group lives in server_group.go.

// --- Core types ---

// Middleware is an HTTP middleware function.
// It receives the downstream handler and returns the wrapped handler.
//
// Convention: middleware calls next(w, r) to pass the request downstream; not calling
// it breaks the chain.
//
//	func Logging(next http.HandlerFunc) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        start := time.Now()
//	        next(w, r)
//	        log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
//	    }
//	}
type Middleware func(http.HandlerFunc) http.HandlerFunc

// Route represents one HTTP route, dispatched by the Server once registered.
type Route struct {
	// Method is the HTTP method (e.g. GET, POST); case-insensitive.
	Method string
	// Path is the route path, supporting Go 1.22 ServeMux patterns:
	// /users/{id}, /files/{path...}.
	Path string
	// Handler is the HTTP handler serving this route.
	Handler http.HandlerFunc
}

// ServerConfig is the HTTP server configuration.
// The json tags declare defaults and constraints and are compatible with the conf
// package loading from a configuration file.
type ServerConfig struct {
	// Host is the listen address, default "0.0.0.0".
	Host string `json:",default=0.0.0.0"`
	// Port is the listen port, default 8080.
	Port int `json:",default=8080,range=[1:65535]"`
	// CertFile is the TLS certificate file path (optional; setting it enables HTTPS).
	CertFile string `json:",optional"`
	// KeyFile is the TLS private key file path (optional).
	KeyFile string `json:",optional"`
	// ReadTimeout is the read timeout, default 10s.
	// Setting 0 via ServerConfig is treated as "unset" and the default applies;
	// use WithReadTimeout(0) to really set it to 0 (no limit).
	ReadTimeout time.Duration `json:",default=10s"`
	// WriteTimeout is the write timeout, default 10s.
	// Setting 0 via ServerConfig is treated as "unset" and the default applies;
	// use WithWriteTimeout(0) to really set it to 0 (no limit).
	WriteTimeout time.Duration `json:",default=10s"`
	// IdleTimeout is the idle connection timeout, default 120s.
	// Setting 0 via ServerConfig is treated as "unset" and the default applies;
	// use WithIdleTimeout(0) to really set it to 0 (no limit).
	IdleTimeout time.Duration `json:",default=120s"`
	// MaxHeaderBytes is the maximum request header size, default 1MB.
	MaxHeaderBytes int `json:",default=1048576"`
	// ShutdownTimeout is the graceful shutdown timeout, default 10s.
	// Setting 0 via ServerConfig is treated as "unset" and the default applies;
	// use WithShutdownTimeout(0) to really set it to 0.
	ShutdownTimeout time.Duration `json:",default=10s"`
}

// fillDefault fills in the defaults, then overrides them with the non-zero fields from
// the user configuration.
// It delegates to mapping.FillAndOverride, where a zero value counts as "unset" and
// keeps the default.
// To set a value to 0 explicitly use the matching RunOption, such as
// WithReadTimeout(0).
func fillDefault(cfg ServerConfig) ServerConfig {
	var c ServerConfig
	mp.MustFillAndOverride(&c, cfg)
	return c
}

// --- Internal types ---

// Server is an HTTP server supporting route registration, middleware and graceful
// shutdown.
//
// It is built on http.ServeMux, which natively supports:
//   - Method matching (GET / POST / PUT ...) with automatic 405 Method Not Allowed
//   - Path parameters (/users/{id}), read via r.PathValue("id")
//   - Wildcard paths (/files/{path...}), read via r.PathValue("path")
//   - Automatic 404 Not Found
type Server struct {
	conf      ServerConfig
	mux       *http.ServeMux
	gmw       []Middleware
	gh        http.Handler // root handler with global middleware applied, nil means rebuild
	handlerLk sync.Mutex   // guards gh lazy init and rebuild (Use / SetNotFoundHandler concurrency)
	tlsConfig *tls.Config

	// stateLk guards the following mutable state:
	//   - routes: written by AddRoutes, read by Routes/PrintRoutes
	//   - httpServer: written by Start, read by Shutdown/Stop
	//
	// Both may be accessed concurrently by multiple goroutines (for example a
	// service.ServiceGroup starting in one goroutine and stopping in another), and
	// without protection that would be a data race.
	stateLk    sync.RWMutex
	routes     []Route
	httpServer *http.Server

	// notFoundHandler is the custom 404 response handler (optional), set via
	// SetNotFoundHandler.
	notFoundHandler http.HandlerFunc
}

// routesSnapshot returns a copy of the route list (callers must not assume it stays
// unchanged afterwards).
func (s *Server) routesSnapshot() []Route {
	s.stateLk.RLock()
	defer s.stateLk.RUnlock()
	out := make([]Route, len(s.routes))
	copy(out, s.routes)
	return out
}

// currentHTTPServer returns the currently running http.Server, or nil when not
// started.
// It is used by Shutdown/Stop to avoid racing with the write in Start.
func (s *Server) currentHTTPServer() *http.Server {
	s.stateLk.RLock()
	defer s.stateLk.RUnlock()
	return s.httpServer
}

// --- Server construction and route registration ---

// NewServer creates an HTTP server.
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

// AddRoute adds a single route and may carry RouteOption values.
func (s *Server) AddRoute(r Route, opts ...RouteOption) {
	s.AddRoutes([]Route{r}, opts...)
}

// AddRoutes adds a group of routes.
//
// The RouteOption values in opts are applied to this whole group of routes (such as
// prefix and middleware).
//
// Middleware execution order: global middleware (added via Use) → group middleware
// (added via WithMiddleware) → route handler.
func (s *Server) AddRoutes(rs []Route, opts ...RouteOption) {
	g := routeGroup{routes: rs}
	for _, opt := range opts {
		opt(&g)
	}

	for _, r := range g.routes {
		handler := r.Handler

		// Apply group middleware (wrap in reverse so the first added runs first)
		for i := len(g.middlewares) - 1; i >= 0; i-- {
			handler = g.middlewares[i](handler)
		}
		// Global middleware is not baked in at registration time but applied
		// dynamically per request by gh, so global middleware added via Use also
		// affects already-registered routes.

		pattern := buildPattern(r.Method, r.Path)
		s.mux.HandleFunc(pattern, handler)

		s.stateLk.Lock()
		s.routes = append(s.routes, Route{
			Method:  strings.ToUpper(r.Method),
			Path:    r.Path,
			Handler: r.Handler, // keep the raw handler so PrintRoutes can reflect its name
		})
		s.stateLk.Unlock()
	}
}

// Use adds global middleware, affecting all previously and subsequently registered
// routes.
// Several middleware run in the order they were added (first added runs first).
//
// Global middleware is applied dynamically per request (wrapping the whole router), so
// even when routes are registered first and Use is called afterwards, the already
// registered routes still pass through the newly added global middleware.
// Note: calling Use after Start has begun does not affect the running httpServer.
func (s *Server) Use(mws ...Middleware) {
	s.handlerLk.Lock()
	s.gmw = append(s.gmw, mws...)
	s.gh = nil // clear the cache so the next Handler() rebuilds it
	s.handlerLk.Unlock()
}

// Routes returns all registered routes (with the raw, unwrapped handlers).
// It returns a copy, so modifying it does not affect the Server's internal state; it
// is concurrency-safe.
func (s *Server) Routes() []Route {
	return s.routesSnapshot()
}

// PrintRoutes prints the list of registered routes.
//
//	server.PrintRoutes()
//	// Output:
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

	// Compute the column widths
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

// handlerName obtains the function name of an http.HandlerFunc by reflection
// (including the package path).
// It returns an empty string when it cannot be obtained.
func handlerName(h http.HandlerFunc) string {
	if h == nil {
		return ""
	}
	return runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
}

// Mux returns the underlying ServeMux for advanced scenarios (such as registering
// routes manually).
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Handler returns the server's HTTP Handler, usable in httptest and similar
// scenarios.
// The returned handler already has the global middleware (added via Use) applied.
// It is concurrency-safe: the lazy build is protected by an internal lock.
func (s *Server) Handler() http.Handler {
	s.handlerLk.Lock()
	defer s.handlerLk.Unlock()
	if s.gh == nil {
		s.buildGlobalHandler()
	}
	return s.gh
}

// buildGlobalHandler builds the root handler with global middleware and the custom 404
// applied (lazily).
// Global middleware runs in the order added (first added runs first), wrapping the
// entire mux.
// Group middleware is already baked into each route handler at registration time, so
// the execution order is: global middleware → group middleware → route handler.
//
// The handler chain (innermost to outermost):
//
//	mux (including custom 404 detection) → global middleware → error rendering/route
//	template injection
func (s *Server) buildGlobalHandler() {
	handler := http.HandlerFunc(s.mux.ServeHTTP)
	// The custom 404 sits right next to the mux so it only applies to genuinely
	// unmatched routes.
	// "Unmatched" is decided by a routing pre-check rather than by wrapping the
	// ResponseWriter, because the latter cannot tell "route not matched" apart from
	// "the handler deliberately returned 404" and would hijack those business 404s;
	// wrapping the writer would also drop optional capabilities such as
	// Flush/Hijack/Push/Unwrap, silently breaking SSE, WebSocket and HTTP/2 Push.
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

	// Global middleware (wrapped in reverse so the first added runs first)
	for i := len(s.gmw) - 1; i >= 0; i-- {
		handler = s.gmw[i](handler)
	}

	// Route template injection must wrap the **outermost** layer of the middleware
	// chain: global middleware sits outside the mux, and at that point net/http has
	// not yet written the matched route template into r.Pattern (ServeMux only fills
	// it when dispatching to the matched handler).
	// Middleware that needs to aggregate per route (circuit breaking, metrics) would
	// therefore not see a stable template.
	// Here we do a routing pre-check, put the template into the context, and only then
	// enter the middleware chain so middleware.PatternFromContext can read it.
	//
	// Order matters: wrapped inside, the context would still lack the template when
	// middleware runs.
	mux := s.mux
	inner := handler
	// Only pre-check routing when global middleware/custom 404 exist (they are the
	// only ones needing the template).
	needPattern := len(s.gmw) > 0 || s.notFoundHandler != nil
	s.gh = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject error rendering per request: requests dispatched through httpx keep
		// the unified JSON response, while gin/echo routes in the same process stay
		// unaffected (see middlewareErrorHandler).
		ctx := middleware.ContextWithErrorHandler(r.Context(), middlewareErrorHandler)
		if needPattern {
			if pattern := matchedPattern(mux, r); pattern != "" {
				ctx = middleware.ContextWithPattern(ctx, pattern)
			}
		}
		inner(w, r.WithContext(ctx))
	})
}

// matchedPattern returns the route template this request will be dispatched to by the
// mux (e.g. "GET /users/{id}").
// It returns an empty string when unmatched (including 405).
//
// Note: it only queries via mux.Handler and never calls mux.ServeHTTP, so it does not
// interfere with ServeMux's later population of r.Pattern.
func matchedPattern(mux *http.ServeMux, r *http.Request) string {
	_, pattern := mux.Handler(r)
	return pattern
}

// notFoundHandlerPtr is the code pointer of net/http's built-in 404 handler
// (http.NotFoundHandler()).
var notFoundHandlerPtr = reflect.ValueOf(http.NotFoundHandler()).Pointer()

// isMuxNotFound reports whether the mux will hand this request to the built-in 404
// handler.
//
// Looking at the pattern returned by ServeMux.Handler alone is not enough: Go 1.22+
// returns an empty pattern both for "no match" and for "path matched but method not
// allowed (405)", so judging by pattern alone would mistake a 405 for a 404 (losing
// the Allow header). Both conditions must hold: an empty pattern *and* a returned
// handler that is exactly the built-in NotFoundHandler.
func isMuxNotFound(mux *http.ServeMux, r *http.Request) bool {
	h, pattern := mux.Handler(r)
	if pattern != "" {
		return false
	}
	v := reflect.ValueOf(h)
	// The matched type may be any http.Handler implementation (not necessarily a
	// function); in that case it cannot be the built-in 404.
	if v.Kind() != reflect.Func {
		return false
	}
	return v.Pointer() == notFoundHandlerPtr
}

// --- Custom error responses ---

// SetNotFoundHandler sets a custom response handler for route-not-found (404).
// Every request not matched by any route goes to this handler instead of the default
// "404 page not found".
// Note: the handler must write the status code itself; if WriteHeader is never called,
// net/http implicitly writes 200 (which is exactly the convention followed by the
// httpx.OkJSON family: HTTP 200 plus a business error code in the body).
// A 404 deliberately returned from inside a business route is not hijacked and reaches
// the client unchanged.
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

// --- Startup and shutdown ---

// Start starts the HTTP server with graceful shutdown support.
//
// The server runs in a dedicated goroutine while the main goroutine blocks waiting for
// a signal.
// On SIGINT (Ctrl+C), SIGTERM or SIGHUP it performs a graceful shutdown.
//
// When CertFile and KeyFile are configured, it starts an HTTPS service.
//
// Concurrency semantics: Start registers the http.Server before it begins listening,
// and the registration is lock-protected, so another goroutine calling Stop/Shutdown
// cannot race with it; but if Stop completes before Start, that Stop is a no-op (the
// server was not running yet).
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

	// Register before listening: Stop/Shutdown read it via currentHTTPServer(), and
	// an unlocked write would be a data race with them (detectable with -race).
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
	// Listen for SIGINT, SIGTERM and SIGHUP to stay compatible with Kubernetes signals
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	// Unregistering is mandatory: otherwise sigCh stays registered with the signal
	// package after this function returns, later signals get delivered to a channel
	// nobody receives from (silently dropped once the buffer is full), and repeated
	// Start calls in the same process accumulate registrations.
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

// Shutdown gracefully shuts down the server, waiting for active connections to finish.
// The timeout is set by WithShutdownTimeout (10 seconds by default).
// A failed shutdown is logged but the error is not silently swallowed.
//
// If the server has not been started (Start was never called), it returns nil (no-op).
// It is concurrency-safe and may be called from a goroutine other than the one running
// Start.
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

// Stop stops the server and returns the shutdown error, delegating to Shutdown.
// It provides a "stop"-named entry point equivalent to Shutdown, making it easy to
// satisfy management interfaces that require Stop() error (e.g. pairing with
// service.AsService to join a ServiceGroup); direct callers can obtain the error just
// like with Shutdown.
func (s *Server) Stop() error {
	return s.Shutdown()
}

// --- Internal helper functions ---

// buildPattern builds the ServeMux route pattern (format: "METHOD /path").
// It normalizes the path automatically: a non-empty path is guaranteed to start with
// "/", otherwise http.ServeMux panics on an invalid pattern (such as "GET users").
func buildPattern(method, path string) string {
	method = strings.ToUpper(method)
	path = normalizePath(path)
	if method == "" || method == "*" {
		return path
	}
	return method + " " + path
}

// normalizePath normalizes a route path:
//   - an empty path is treated as the root path "/"
//   - a non-empty path is guaranteed to start with "/" (a leading slash is added)
func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// joinPath joins a prefix and a path, dealing with redundant slashes.
// When p ends with "/" (subtree match pattern such as /static/), the trailing slash is
// preserved; a bare "/" is the exception.
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
