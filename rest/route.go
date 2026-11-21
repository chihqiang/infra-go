package rest

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/chihqiang/ten-go/rest/chain"
	"github.com/chihqiang/ten-go/rest/handler"
	"github.com/chihqiang/ten-go/rest/router"
)

// Route describes an HTTP endpoint.
type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

// RouteOption customizes a single route before it is registered.
type RouteOption func(*Route)

// WithRouteMiddleware wraps the route handler with middleware.
func WithRouteMiddleware(middleware ...chain.Middleware) RouteOption {
	return func(route *Route) {
		if route.Handler == nil || len(middleware) == 0 {
			return
		}
		route.Handler = chain.New(middleware...).Then(route.Handler).ServeHTTP
	}
}

// WithRouteAuth requires a valid bearer token signed with secret. Pass
// handler.AuthOption values for a previous secret, callback or skip paths; for
// server-wide auth use WithAuth.
func WithRouteAuth(secret string, opts ...handler.AuthOption) RouteOption {
	return WithRouteMiddleware(handler.NewAuthHandler(
		handler.AuthConfig{Secret: secret}, opts...).Handle)
}

func applyRouteOptions(route Route, opts []RouteOption) Route {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&route)
	}
	return route
}

// AddRoute registers a route with the server after applying opts.
func (s *Server) AddRoute(route Route, opts ...RouteOption) {
	route = applyRouteOptions(route, opts)
	s.HandleFunc(route.Method, route.Path, route.Handler)
}

// AddRoutes registers multiple routes with the server.
func (s *Server) AddRoutes(routes ...Route) {
	for _, route := range routes {
		s.AddRoute(route)
	}
}

// Routes returns the registered routes in registration order.
func (s *Server) Routes() []Route {
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	return append([]Route(nil), s.routes...)
}

// PrintRoutes writes the registered routes as "METHOD PATH" lines, sorted for
// stable output.
func (s *Server) PrintRoutes(w io.Writer) error {
	if w == nil {
		return nil
	}

	routes := s.Routes()
	lines := make([]string, 0, len(routes))
	for _, route := range routes {
		lines = append(lines, strings.TrimSpace(route.Method+" "+route.Path))
	}
	sort.Strings(lines)

	_, err := fmt.Fprintf(w, "Routes:\n  %s\n", strings.Join(lines, "\n  "))
	return err
}

// RouteGroup registers routes under a shared path prefix.
type RouteGroup struct {
	server      *Server
	prefix      string
	parent      *RouteGroup
	middlewares []chain.Middleware
}

// Group creates a route group under the given prefix.
func (s *Server) Group(prefix string) *RouteGroup {
	return &RouteGroup{server: s, prefix: router.NormalizePath(prefix)}
}

// Group creates a nested route group under the current prefix. Middleware is
// resolved when a route is registered, so a Use call on the parent also applies
// to routes added through an already-created child group.
func (g *RouteGroup) Group(prefix string) *RouteGroup {
	return &RouteGroup{
		server: g.server,
		prefix: router.JoinPath(g.prefix, prefix),
		parent: g,
	}
}

// Use adds middleware to routes registered with the group after this call.
func (g *RouteGroup) Use(middlewares ...chain.Middleware) {
	g.middlewares = append(g.middlewares, middlewares...)
}

// Handle registers a handler under the group's prefix.
func (g *RouteGroup) Handle(method, routePath string, handler http.HandlerFunc) {
	g.HandleFunc(method, routePath, handler)
}

// HandleFunc registers a handler function under the group's prefix.
func (g *RouteGroup) HandleFunc(method, routePath string, handler http.HandlerFunc) {
	wrapped := g.wrap(handler)
	routePath = router.JoinPath(g.prefix, routePath)
	g.server.HandleFunc(method, routePath, wrapped)
	g.registerRootAlias(method, routePath, wrapped)
}

// AddRoute registers a route under the group's prefix after applying opts.
func (g *RouteGroup) AddRoute(route Route, opts ...RouteOption) {
	route = applyRouteOptions(route, opts)
	route.Path = router.JoinPath(g.prefix, route.Path)
	route.Handler = g.wrap(route.Handler)
	g.server.AddRoute(route)
	g.registerRootAlias(route.Method, route.Path, route.Handler)
}

// registerRootAlias also serves the trailing-slash form of a group root route
// ("/api/" for Group("/api").Handle("/")). The mux pattern "{$}" keeps the
// match exact, so unknown paths below the prefix still return 404 instead of
// falling through to the root handler.
func (g *RouteGroup) registerRootAlias(method, routePath string, handler http.HandlerFunc) {
	prefix := strings.TrimSuffix(g.prefix, "/")
	if prefix == "" || routePath != prefix {
		return
	}
	g.server.register(method, routePath+"/{$}", handler, false)
}

// AddRoutes registers multiple routes under the group's prefix.
func (g *RouteGroup) AddRoutes(routes ...Route) {
	for _, route := range routes {
		g.AddRoute(route)
	}
}

func (g *RouteGroup) wrap(handler http.HandlerFunc) http.HandlerFunc {
	middlewares := g.middlewareChain()
	if handler == nil || len(middlewares) == 0 {
		return handler
	}
	return chain.New(middlewares...).Then(handler).ServeHTTP
}

// middlewareChain resolves inherited middleware at registration time so a
// parent's later Use call is not lost in already-created child groups.
func (g *RouteGroup) middlewareChain() []chain.Middleware {
	var inherited []chain.Middleware
	if g.parent != nil {
		inherited = g.parent.middlewareChain()
	}
	middlewares := make([]chain.Middleware, 0, len(inherited)+len(g.middlewares))
	middlewares = append(middlewares, inherited...)
	middlewares = append(middlewares, g.middlewares...)
	return middlewares
}
