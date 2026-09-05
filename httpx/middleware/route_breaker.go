package middleware

import (
	"fmt"
	"net/http"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/logger"
)

// RouteBreaker 是按路由隔离的熔断中间件。
// 每个路由（METHOD:path）拥有独立的熔断器，统计互不影响，
// 避免单个路由的失败拉低其他路由的通过率。
type RouteBreaker struct{}

// NewRouteBreaker 创建按路由隔离的熔断中间件。
func NewRouteBreaker() *RouteBreaker {
	return &RouteBreaker{}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的按路由熔断中间件。
// 熔断器通过 breaker.GetBreaker 按名称缓存，同名路由共享同一实例。
// 熔断打开时返回 503 Service Unavailable；请求成功（<500）上报 Accept，
// 请求失败（>=500）上报 Reject。
func (b *RouteBreaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 以 METHOD:path 作为熔断器名称，按路由隔离
			name := r.Method + ":" + r.URL.Path
			brk := breaker.GetBreaker(name)

			promise, err := brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by route breaker",
					logger.String("breaker", name),
					logger.String("path", r.URL.Path),
					logger.String("remote", r.RemoteAddr),
					logger.Err(err),
				)
				writeError(r.Context(), w, http.StatusServiceUnavailable, "service unavailable")
				return
			}

			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			if rec.Status() < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.Status(), http.StatusText(rec.Status())))
			}
		})
	}
}
