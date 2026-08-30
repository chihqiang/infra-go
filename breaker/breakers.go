package breaker

import (
	"context"
	"sync"
)

var (
	breakersLock sync.RWMutex
	breakers     = make(map[string]Breaker)
)

// GetBreaker 返回指定名称的熔断器，不存在时创建并缓存。
// 同名熔断器全局共享，保证同一目标（下游服务/接口）的统计一致。
func GetBreaker(name string) Breaker {
	breakersLock.RLock()
	b, ok := breakers[name]
	breakersLock.RUnlock()
	if ok {
		return b
	}

	breakersLock.Lock()
	defer breakersLock.Unlock()
	b, ok = breakers[name]
	if !ok {
		b = NewBreaker(WithName(name))
		breakers[name] = b
	}
	return b
}

// NoBreakerFor 为指定名称注册一个不熔断的实现（关闭熔断保护）。
func NoBreakerFor(name string) {
	breakersLock.Lock()
	breakers[name] = NopBreaker()
	breakersLock.Unlock()
}

// Do 使用指定名称的熔断器执行请求。
func Do(name string, req func() error) error {
	return GetBreaker(name).Do(req)
}

// DoCtx 同 Do，支持 context。
func DoCtx(ctx context.Context, name string, req func() error) error {
	return GetBreaker(name).DoCtx(ctx, req)
}

// DoWithAcceptable 使用指定名称的熔断器执行请求，支持自定义错误可接受策略。
func DoWithAcceptable(name string, req func() error, acceptable Acceptable) error {
	return GetBreaker(name).DoWithAcceptable(req, acceptable)
}

// DoWithAcceptableCtx 同 DoWithAcceptable，支持 context。
func DoWithAcceptableCtx(ctx context.Context, name string, req func() error,
	acceptable Acceptable) error {
	return GetBreaker(name).DoWithAcceptableCtx(ctx, req, acceptable)
}

// DoWithFallback 使用指定名称的熔断器执行请求，熔断打开时执行 fallback。
func DoWithFallback(name string, req func() error, fallback Fallback) error {
	return GetBreaker(name).DoWithFallback(req, fallback)
}

// DoWithFallbackCtx 同 DoWithFallback，支持 context。
func DoWithFallbackCtx(ctx context.Context, name string, req func() error,
	fallback Fallback) error {
	return GetBreaker(name).DoWithFallbackCtx(ctx, req, fallback)
}

// DoWithFallbackAcceptable 使用指定名称的熔断器执行请求，支持降级与错误可接受策略。
func DoWithFallbackAcceptable(name string, req func() error, fallback Fallback,
	acceptable Acceptable) error {
	return GetBreaker(name).DoWithFallbackAcceptable(req, fallback, acceptable)
}

// DoWithFallbackAcceptableCtx 同 DoWithFallbackAcceptable，支持 context。
func DoWithFallbackAcceptableCtx(ctx context.Context, name string, req func() error,
	fallback Fallback, acceptable Acceptable) error {
	return GetBreaker(name).DoWithFallbackAcceptableCtx(ctx, req, fallback, acceptable)
}
