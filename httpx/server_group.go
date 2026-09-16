package httpx

// This file holds the route group Group: it organizes routes on a Server by path
// prefix plus middleware, and supports chaining and nesting (child groups inherit the
// parent's prefix and middleware).
//
// Dependencies: Group only uses symbols from this package (Server / Route /
// RouteOption / WithPrefix / WithMiddlewares / Middleware / joinPath) and has no
// third-party dependencies.

// --- Route group (Group) ---

// Group is a route group sharing a path prefix and middleware.
// It supports chaining and nesting, which makes it easy to organize routes by module.
type Group struct {
	server      *Server
	prefix      string
	middlewares []Middleware
}

// Group creates a route group.
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

// Use adds middleware to the route group.
func (g *Group) Use(mws ...Middleware) {
	g.middlewares = append(g.middlewares, mws...)
}

// AddRoute adds a single route to the route group and may carry RouteOption values.
// The RouteOption values in opts are applied after the group prefix and group
// middleware.
func (g *Group) AddRoute(r Route, opts ...RouteOption) {
	g.AddRoutes([]Route{r}, opts...)
}

// AddRoutes adds several routes to the route group and may carry RouteOption values.
// The route paths have the group prefix prepended automatically and the handlers get
// the group middleware applied.
// The RouteOption values in opts are applied after the group prefix and group
// middleware.
func (g *Group) AddRoutes(rs []Route, opts ...RouteOption) {
	allOpts := []RouteOption{WithPrefix(g.prefix)}
	if len(g.middlewares) > 0 {
		allOpts = append(allOpts, WithMiddlewares(g.middlewares...))
	}
	allOpts = append(allOpts, opts...)
	g.server.AddRoutes(rs, allOpts...)
}

// Group creates a child route group, inheriting the parent's prefix and middleware.
//
//	api := server.Group("/api", logMiddleware)
//	v1 := api.Group("/v1", authMiddleware)
//	// route prefix /api/v1, middleware logMiddleware → authMiddleware
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		server:      g.server,
		prefix:      joinPath(g.prefix, prefix),
		middlewares: append(append([]Middleware(nil), g.middlewares...), mws...),
	}
}
