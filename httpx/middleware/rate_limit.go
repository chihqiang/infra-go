package middleware

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/httpx/match"
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
type RateLimit struct {
	limiter  RateLimiter
	disabled bool // limiter 为 nil 时降级为不限流（fail-open）
	matcher  *match.PathMatcher
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
		matcher: match.NewPathMatcher(skipPaths),
	}
	if limiter == nil {
		// limiter 缺失：不 panic，仅告警并降级为不限流（fail-open）。
		// 注意必须放行，否则运行期调用 nil 接口方法会 panic。
		rl.disabled = true
		logger.Warn("middleware: NewRateLimit called with nil limiter, rate limiting disabled")
	}
	return rl
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
					logger.String("remote", r.RemoteAddr),
				)
				writeError(r.Context(), w, http.StatusTooManyRequests, http.StatusText(http.StatusTooManyRequests))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
