package middleware

import (
	"fmt"
	"net/http"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/logger"
)

// Breaker 是熔断中间件，保护下游 handler 不被级联拖垮。
// 基于 breaker 模块的 Google SRE 算法；构造时创建一个以 "http" 为名的全局熔断器，
// 所有请求共享该实例（如需按路由隔离请使用 RouteBreaker）。
type Breaker struct {
	brk breaker.Breaker
}

// NewBreaker 创建熔断中间件。
func NewBreaker() *Breaker {
	return &Breaker{brk: breaker.NewBreaker(breaker.WithName("http"))}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的熔断中间件。
// 熔断打开时返回 503 Service Unavailable；请求成功（<500）上报 Accept，
// 请求失败（>=500）上报 Reject，用于驱动熔断状态。
func (b *Breaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			promise, err := b.brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by breaker",
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
