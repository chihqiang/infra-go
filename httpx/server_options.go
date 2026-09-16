package httpx

import (
	"crypto/tls"
	"net/http"
	"time"
)

// This file holds the various options (With*) and middleware wrappers (Apply*) for
// the Server / route groups:
//   - Option types (RouteOption / RunOption) and the routeGroup that RouteOption acts on
//   - Route group options (WithPrefix / WithMiddleware / WithMiddlewares)
//   - Standalone middleware wrappers (ApplyMiddleware / ApplyMiddlewares)
//   - Standard middleware adaptation (AsMiddleware)
//   - Server options (WithReadTimeout / WithWriteTimeout / WithIdleTimeout /
//     WithMaxHeaderBytes / WithTLSConfig / WithShutdownTimeout)

// --- Option types ---

// RouteOption customizes a group of routes, such as their prefix and middleware.
type RouteOption func(*routeGroup)

// RunOption customizes the Server, such as its timeouts and TLS.
// You can also pass a closure directly to register routes or add middleware at
// construction time:
//
//	server := httpx.NewServer(conf, func(s *httpx.Server) {
//	    s.Use(loggingMiddleware)
//	    s.AddRoute(httpx.Route{Method: "GET", Path: "/ping", Handler: ping})
//	})
type RunOption func(*Server)

// routeGroup is a group of routes together with their configuration; it is what
// RouteOption acts on.
type routeGroup struct {
	routes      []Route
	middlewares []Middleware
}

// --- Route group options (RouteOption) ---

// WithPrefix adds a path prefix to the route group.
//
//	server.AddRoutes([]Route{
//	    {Method: "GET", Path: "/users", Handler: listUsers},
//	    {Method: "POST", Path: "/users", Handler: createUser},
//	}, httpx.WithPrefix("/api/v1"))
//
// The registered routes are: GET /api/v1/users, POST /api/v1/users
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

// WithMiddleware adds a single middleware to the route group.
// Middleware runs in the order it was added (first added runs first).
func WithMiddleware(mw Middleware) RouteOption {
	return func(g *routeGroup) {
		g.middlewares = append(g.middlewares, mw)
	}
}

// WithMiddlewares adds several middleware to the route group.
// Middleware runs in the order given (the first one runs first).
func WithMiddlewares(mws ...Middleware) RouteOption {
	return func(g *routeGroup) {
		g.middlewares = append(g.middlewares, mws...)
	}
}

// --- Standalone middleware wrappers ---

// ApplyMiddleware applies a middleware to routes and returns the wrapped routes.
// It suits cases where specific routes need wrapping before being added.
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

// ApplyMiddlewares applies several middleware to routes and returns the wrapped
// routes.
// Middleware runs in slice order (the first one runs first).
func ApplyMiddlewares(mws []Middleware, rs ...Route) []Route {
	for i := len(mws) - 1; i >= 0; i-- {
		rs = ApplyMiddleware(mws[i], rs...)
	}
	return rs
}

// --- Standard middleware adaptation ---

// AsMiddleware adapts a standard middleware into httpx.Middleware, making it easy to
// register any func(http.Handler) http.Handler middleware on the server (such as
// NewXxx().Middleware() from the httpx/middleware subpackage, or any other third-party
// net/http-based middleware).
//
//	// custom/third-party standard middleware → httpx middleware
//	server.Use(httpx.AsMiddleware(myStdMiddleware))
//
//	// the httpx/middleware subpackage (OO style) can also be wired up this way:
//	server.Use(httpx.AsMiddleware(middleware.NewCORS("*").Middleware()))
func AsMiddleware(mw func(http.Handler) http.Handler) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return mw(http.HandlerFunc(next)).ServeHTTP
	}
}

// --- Server options (RunOption) ---

// WithReadTimeout sets the read timeout. It overrides ReadTimeout from the config.
func WithReadTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.ReadTimeout = d
	}
}

// WithWriteTimeout sets the write timeout. It overrides WriteTimeout from the config.
func WithWriteTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.WriteTimeout = d
	}
}

// WithIdleTimeout sets the idle connection timeout. It overrides IdleTimeout from the
// config.
func WithIdleTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.IdleTimeout = d
	}
}

// WithMaxHeaderBytes sets the maximum request header size. It overrides
// MaxHeaderBytes from the config.
func WithMaxHeaderBytes(n int) RunOption {
	return func(s *Server) {
		s.conf.MaxHeaderBytes = n
	}
}

// WithTLSConfig sets the TLS configuration.
func WithTLSConfig(cfg *tls.Config) RunOption {
	return func(s *Server) {
		s.tlsConfig = cfg
	}
}

// WithShutdownTimeout sets the graceful shutdown timeout, overriding ShutdownTimeout
// from the config.
func WithShutdownTimeout(d time.Duration) RunOption {
	return func(s *Server) {
		s.conf.ShutdownTimeout = d
	}
}
