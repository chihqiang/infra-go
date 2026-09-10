package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// Breaker 是熔断中间件，保护下游 handler 不被级联拖垮。
// 基于 breaker 模块的 Google SRE 算法；构造时创建一个以 "http" 为名的全局熔断器，
// 所有请求共享该实例（如需按路由隔离请使用 RouteBreaker）。
type Breaker struct {
	brk breaker.Breaker
	// retryAfter 为熔断打开时提示客户端的重试间隔，默认 breakerRetryAfter。
	retryAfter time.Duration
}

// breakerRetryAfter 是熔断打开时的默认重试提示间隔。
//
// 与熔断算法的半开探测周期对齐（breaker 内部 forcePassDuration 为 1 秒）：
// 熔断打开后最多等待该时长就会放行一个探测请求，
// 因此这是客户端最早可能成功的时间点（RFC 9110 §15.6.4 建议 503 给出该提示）。
const breakerRetryAfter = time.Second

// NewBreaker 创建熔断中间件。
func NewBreaker() *Breaker {
	return &Breaker{
		brk:        breaker.NewBreaker(breaker.WithName("http")),
		retryAfter: breakerRetryAfter,
	}
}

// WithRetryAfter 设置熔断打开时的 Retry-After 提示间隔（RFC 9110 §10.2.3）。
// d <= 0 表示不发送该头。
func (b *Breaker) WithRetryAfter(d time.Duration) *Breaker {
	b.retryAfter = d
	return b
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的熔断中间件。
// 熔断打开时返回 503 Service Unavailable（并带 Retry-After）；
// 请求成功（<500）上报 Accept，请求失败（>=500）上报 Reject，用于驱动熔断状态。
func (b *Breaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			promise, err := b.brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by breaker",
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
					logger.Err(err),
				)
				// RFC 9110 §15.6.4：经历负载/故障的服务器 SHOULD 发送 Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					b.retryAfter, "service unavailable")
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
