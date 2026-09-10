package middleware

import "context"

// routePatternCtxKey 是路由模板在 context 中的键。
// 使用独立的未导出类型，避免与其它包的 key 冲突。
type routePatternCtxKey struct{}

// PatternFromContext 返回请求匹配到的路由模板（如 "GET /users/{id}"）。
//
// 为什么需要它：全局中间件包在 ServeMux **外层**，此时 net/http 尚未把匹配到的
// 模板写入 r.Pattern（那是 ServeMux 在分发到命中 handler 时才做的事），
// 因此在全局中间件里读 r.Pattern 恒为空。
// httpx.Server 会在进入中间件链之前完成路由预判并把模板放入 context，
// 于是需要"按路由聚合"的中间件（如熔断、指标）能拿到稳定的模板而非具体路径。
//
// 未设置时返回 ""。
func PatternFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	pattern, _ := ctx.Value(routePatternCtxKey{}).(string)
	return pattern
}

// ContextWithPattern 把路由模板写入 context。
// 主要由 httpx.Server 内部使用；其它框架可自行调用，
// 以便复用依赖路由模板的中间件。
func ContextWithPattern(ctx context.Context, pattern string) context.Context {
	if pattern == "" {
		return ctx
	}
	return context.WithValue(ctx, routePatternCtxKey{}, pattern)
}
