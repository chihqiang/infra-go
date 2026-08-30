package breaker

import (
	"context"
	"errors"
	"time"
)

// ErrServiceUnavailable 熔断器打开（拒绝请求）时返回的错误。
var ErrServiceUnavailable = errors.New("breaker: service unavailable")

// Acceptable 判断错误是否可接受（不计入失败统计）。
// 例如 4xx 业务错误通常是调用方问题，不应触发熔断：
//
//	b.DoWithAcceptable(req, func(err error) bool {
//	    return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized)
//	})
type Acceptable func(err error) bool

// Fallback 熔断器打开时执行的降级逻辑。
type Fallback func(err error) error

// Promise 由 Breaker.Allow 返回，调用方执行完请求后需回调：
//   - 成功：调用 Accept()
//   - 失败：调用 Reject(reason)，reason 用于日志记录失败原因
type Promise interface {
	// Accept 告知熔断器本次调用成功。
	Accept()
	// Reject 告知熔断器本次调用失败。
	// reason 为失败原因，熔断打开时会输出最近若干条失败原因。
	Reject(reason string)
}

// Breaker 定义熔断器接口，基于 Google SRE 自适应过载算法。
//
// 三种状态：
//   - 关闭（Closed）：错误率低于阈值，正常处理并持续统计
//   - 打开（Open）：错误率超过阈值，后续请求快速失败（返回 ErrServiceUnavailable）
//   - 半开（Half-Open）：冷却期后放行探测请求，成功则关闭熔断器
//
// 用法：
//
//	b := breaker.NewBreaker(breaker.WithName("payment-gateway"))
//	err := b.Do(func() error { return callPaymentAPI(req) })
type Breaker interface {
	// Name 返回熔断器名称，用于日志与指标区分。
	Name() string

	// Allow 检查请求是否允许通过。
	// 允许时返回 Promise（调用方完成后须 Accept/Reject），
	// 熔断器打开时返回 ErrServiceUnavailable。
	Allow() (Promise, error)
	// AllowCtx 同 Allow，但 context 已取消时直接返回 context 错误。
	AllowCtx(ctx context.Context) (Promise, error)

	// Do 执行请求；熔断器打开时立即返回 ErrServiceUnavailable，不执行请求。
	// 请求 panic 会被视为失败并重新抛出。
	Do(req func() error) error
	// DoCtx 同 Do，但 context 已取消时直接返回 context 错误。
	DoCtx(ctx context.Context, req func() error) error

	// DoWithAcceptable 同 Do，但通过 acceptable 精确控制哪些错误计入失败统计。
	DoWithAcceptable(req func() error, acceptable Acceptable) error
	// DoWithAcceptableCtx 同 DoWithAcceptable，支持 context。
	DoWithAcceptableCtx(ctx context.Context, req func() error, acceptable Acceptable) error

	// DoWithFallback 同 Do，但熔断器打开时执行 fallback 降级逻辑。
	DoWithFallback(req func() error, fallback Fallback) error
	// DoWithFallbackCtx 同 DoWithFallback，支持 context。
	DoWithFallbackCtx(ctx context.Context, req func() error, fallback Fallback) error

	// DoWithFallbackAcceptable 同 DoWithFallback，并支持自定义错误可接受策略。
	DoWithFallbackAcceptable(req func() error, fallback Fallback, acceptable Acceptable) error
	// DoWithFallbackAcceptableCtx 同 DoWithFallbackAcceptable，支持 context。
	DoWithFallbackAcceptableCtx(ctx context.Context, req func() error, fallback Fallback,
		acceptable Acceptable) error
}

// sreConfig Google SRE 自适应节流算法参数。
// 默认值见 sre.go 中的包常量；通过 Option 可自定义，
// 例如贴近 SRE 官方文档（Handling Overload 第 21 章 Client-Side Throttling）
// 推荐的 2 分钟统计窗口、放大系数 K=2，使用 WithSREDefaults()。
type sreConfig struct {
	window     time.Duration // 统计窗口时长
	k          float64       // 请求放大系数
	minK       float64       // 放大系数下限
	protection int64         // 小流量保护阈值
}

// defaultSREConfig 返回默认的 SRE 算法参数（激进模式：10s 窗口 / K=1.5）。
func defaultSREConfig() sreConfig {
	return sreConfig{
		window:     window,
		k:          k,
		minK:       minK,
		protection: protection,
	}
}

// Option 用于自定义熔断器。
type Option func(*circuitBreaker)

// WithName 设置熔断器名称，用于日志与指标区分。
func WithName(name string) Option {
	return func(b *circuitBreaker) { b.name = name }
}

// WithWindow 设置统计窗口时长。
// 越小对突发响应越快，但对偶发/低流量客户端越敏感；
// SRE 官方文档推荐 2 分钟。默认 10 秒。传入非正值时忽略。
func WithWindow(d time.Duration) Option {
	return func(b *circuitBreaker) {
		if d > 0 {
			b.sre.window = d
		}
	}
}

// WithK 设置请求放大系数 K：当 requests 达到 K*accepts 时开始按概率拒绝。
// 减小 K 更激进（拒绝更多），增大 K 更宽松。SRE 官方文档推荐 2。
// 默认 1.5。传入非正值时忽略。
func WithK(k float64) Option {
	return func(b *circuitBreaker) {
		if k > 0 {
			b.sre.k = k
		}
	}
}

// WithMinK 设置放大系数下限，防止连续失败时权重过低导致过度激进。
// 默认 1.1。传入非正值时忽略。
func WithMinK(k float64) Option {
	return func(b *circuitBreaker) {
		if k > 0 {
			b.sre.minK = k
		}
	}
}

// WithProtection 设置小流量保护阈值：窗口内总请求数低于该值时不被拒绝，
// 避免偶发/低流量客户端被误伤（对应文档对零星请求客户端的告诫）。
// 默认 5。传入负值时忽略。
func WithProtection(n int64) Option {
	return func(b *circuitBreaker) {
		if n >= 0 {
			b.sre.protection = n
		}
	}
}

// WithSREDefaults 使用 SRE 官方文档（Handling Overload 第 21 章
// Client-Side Throttling）推荐的参数：2 分钟统计窗口、放大系数 K=2。
// 相比默认的激进模式（10s / K=1.5），更贴近原始算法：后端资源浪费略多，
// 但状态传播更稳，适合流量平稳的服务。可再叠加 WithWindow/WithK 微调。
func WithSREDefaults() Option {
	return func(b *circuitBreaker) {
		b.sre.window = 2 * time.Minute
		b.sre.k = 2
		b.sre.minK = minK
		b.sre.protection = protection
	}
}

// defaultAcceptable 默认错误可接受策略：仅 nil 错误视为成功。
func defaultAcceptable(err error) bool {
	return err == nil
}
