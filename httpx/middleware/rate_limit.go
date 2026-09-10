package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// RateLimiter 是 HTTP 限流中间件所需的限流器接口。
// 与 ratelimit 包的 Limiter 接口方法集一致，ratelimit.NewTokenBucket /
// NewSlidingWindow / Redis 限流器（*struct）天然满足本接口，无需强依赖
// ratelimit 包（middleware 保持轻量解耦，不引入具体限流实现）。
type RateLimiter interface {
	// Allow 检查是否允许请求通过。
	Allow() bool
	// AllowContext 带 context 的检查，支持超时取消（Redis 限流器借此获得超时控制）。
	AllowContext(ctx context.Context) (bool, error)
}

// RateLimit 是基于限流器的 HTTP 限流中间件。
// 每个请求先向限流器申请配额，允许则放行；被限流返回 429 Too Many Requests。
//
// 被限流时若限流器实现了 RetryAfterProvider，会据其给出精确的 Retry-After
// （RFC 9110 §10.2.3）；否则回退到 WithRetryAfter 配置的值，仍未设置则省略该头。
type RateLimit struct {
	limiter  RateLimiter
	disabled bool // limiter 为 nil 时降级为不限流（fail-open）
	matcher  *x.PathMatcher

	// retryAfter 为限流器无法给出估计时的回退重试间隔，0 表示不发送该头。
	retryAfter time.Duration
}

// NewRateLimit 创建 HTTP 限流中间件。
// limiter 为限流器实现（如 ratelimit.NewTokenBucket），整个服务共享同一实例，
// 按存储与算法自由组合：
//
//   - 单机内存：ratelimit.NewTokenBucket(rate, burst) / NewSlidingWindow(limit, window)
//   - 分布式（多实例共享）：ratelimit.NewRedisTokenBucket / NewRedisSlidingWindow
//   - 一键切换存储：ratelimit.NewTokenBucketWithStore / NewSlidingWindowWithStore
//   - 并发数限制：ratelimit.NewConcurrency
//
// limiter 为 nil 时不限流（直接放行）并记录告警，避免配置遗漏导致服务不可用。
//
// skipPaths 为不参与限流的路径列表，命中直接放行（常用于健康检查等高频探活接口）。
// 匹配方式与 httpx.WithLogger 一致：精确匹配（如 "/healthz"）或以 "*" 结尾的
// 前缀通配（如 "/internal/*"）。
func NewRateLimit(limiter RateLimiter, skipPaths ...string) *RateLimit {
	rl := &RateLimit{
		limiter: limiter,
		matcher: x.NewPathMatcher(skipPaths),
	}
	if limiter == nil {
		// limiter 缺失：不 panic，仅告警并降级为不限流（fail-open）。
		// 注意必须放行，否则运行期调用 nil 接口方法会 panic。
		rl.disabled = true
		logger.Warn("middleware: NewRateLimit called with nil limiter, rate limiting disabled")
	}
	return rl
}

// WithRetryAfter 设置限流器无法给出精确估计时的回退重试间隔。
//
// ratelimit 内置限流器（TokenBucket/SlidingWindow 及其 Redis 版本）
// 均实现了 RetryAfterProvider，因此无需调用本方法即可获得准确的 Retry-After。
// 仅在自定义限流器不实现该接口、且希望仍给出提示时使用。
// d <= 0（默认）表示不发送 Retry-After 头 —— 与其给出编造的时长，不如省略。
func (rl *RateLimit) WithRetryAfter(d time.Duration) *RateLimit {
	rl.retryAfter = d
	return rl
}

// resolveRetryAfter 返回本次限流应提示的重试间隔。
// 优先使用限流器给出的精确值，其次回退到配置值。
func (rl *RateLimit) resolveRetryAfter() time.Duration {
	if p, ok := rl.limiter.(RetryAfterProvider); ok {
		if d := p.RetryAfter(); d > 0 {
			return d
		}
	}
	return rl.retryAfter
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的限流中间件。
//
// 判定使用 AllowContext 并复用请求 context，Redis 限流器因此自动获得超时控制。
// 底层限流器出错（如 Redis 不可用）时选择放行（fail-open），仅记录错误日志，
// 避免限流组件故障拖垮整个服务。若需按 IP/路由/用户等维度独立计数，
// 请按维度 key 自行构建限流器（如 Redis 限流器以 key 区分）后再传入。
func (rl *RateLimit) Middleware() func(http.Handler) http.Handler {
	// limiter 缺失：直接透传（fail-open）
	if rl.disabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 命中跳过规则的路径不参与限流（业务照常处理）
			if rl.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			allowed, err := rl.limiter.AllowContext(r.Context())
			if err != nil {
				// 限流组件异常：fail-open 放行，避免 Redis 抖动拖垮整个服务
				logger.ErrorCtx(r.Context(), "rate limiter check failed, pass through",
					logger.String("path", r.URL.Path),
					logger.Err(err),
				)
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				logger.WarnCtx(r.Context(), "http request dropped by rate limiter",
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
				)
				// RFC 6585 §4：429 可以（MAY）携带 Retry-After 指示何时重试。
				// 客户端退避逻辑普遍依赖该头，故尽量给出精确值。
				WriteRetryAfter(r.Context(), w, http.StatusTooManyRequests,
					rl.resolveRetryAfter(), http.StatusText(http.StatusTooManyRequests))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
