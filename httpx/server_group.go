package httpx

// 本文件承载路由组 Group：在 Server 上按路径前缀 + 中间件组织路由，
// 支持链式调用与嵌套（子组继承父组前缀与中间件）。
//
// 依赖说明：Group 仅使用同包符号（Server / Route / RouteOption /
// WithPrefix / WithMiddlewares / Middleware / joinPath），无第三方依赖。

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
