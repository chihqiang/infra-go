package rest

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/router"
	"github.com/chihqiang/infra-go/rest/token"
)

// Route describes an HTTP endpoint.
type Route struct {
	Method  string
	Path    string
	Handler http.Handler
}

// RouteOption customizes a single route before it is registered.
type RouteOption func(*Route)

// WithRouteMiddleware wraps the route handler with middleware.
func WithRouteMiddleware(middleware ...chain.Middleware) RouteOption {
	return func(route *Route) {
		if route.Handler == nil || len(middleware) == 0 {
			return
		}
		route.Handler = chain.New(middleware...).Then(route.Handler)
	}
}

// WithRouteAuth requires a valid bearer token verified by parser.
func WithRouteAuth(parser *token.Parser, opts ...handler.AuthorizeOption) RouteOption {
	return WithRouteMiddleware(handler.Authorize(parser, opts...))
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
	s.Handle(route.Method, route.Path, route.Handler)
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

	lines := make([]string, 0, len(s.Routes()))
	for _, route := range s.Routes() {
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
	middlewares []chain.Middleware
}

// Group creates a route group under the given prefix.
func (s *Server) Group(prefix string) *RouteGroup {
	return &RouteGroup{server: s, prefix: router.NormalizePath(prefix)}
}

// Group creates a nested route group under the current prefix.
func (g *RouteGroup) Group(prefix string) *RouteGroup {
	return &RouteGroup{
		server:      g.server,
		prefix:      router.JoinPath(g.prefix, prefix),
		middlewares: append([]chain.Middleware(nil), g.middlewares...),
	}
}

// Use adds middleware to routes registered with the group after this call.
func (g *RouteGroup) Use(middlewares ...chain.Middleware) {
	g.middlewares = append(g.middlewares, middlewares...)
}

// Handle registers a handler under the group's prefix.
func (g *RouteGroup) Handle(method, routePath string, handler http.Handler) {
	g.server.Handle(method, router.JoinPath(g.prefix, routePath), g.wrap(handler))
}

// HandleFunc registers a handler function under the group's prefix.
func (g *RouteGroup) HandleFunc(method, routePath string, handler http.HandlerFunc) {
	g.Handle(method, routePath, handler)
}

// AddRoute registers a route under the group's prefix after applying opts.
func (g *RouteGroup) AddRoute(route Route, opts ...RouteOption) {
	route = applyRouteOptions(route, opts)
	route.Path = router.JoinPath(g.prefix, route.Path)
	route.Handler = g.wrap(route.Handler)
	g.server.AddRoute(route)
}

// AddRoutes registers multiple routes under the group's prefix.
func (g *RouteGroup) AddRoutes(routes ...Route) {
	for _, route := range routes {
		g.AddRoute(route)
	}
}

func (g *RouteGroup) wrap(handler http.Handler) http.Handler {
	if handler == nil || len(g.middlewares) == 0 {
		return handler
	}
	return chain.New(g.middlewares...).Then(handler)
}
