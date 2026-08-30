package breaker

import "context"

const nopBreakerName = "nopBreaker"

// nopBreaker 不做任何熔断的实现，请求永远放行。
type nopBreaker struct{}

// NopBreaker 返回一个永不触发熔断的 Breaker。
// 适用于希望关闭某条调用链熔断保护的场景。
func NopBreaker() Breaker {
	return nopBreaker{}
}

func (b nopBreaker) Name() string { return nopBreakerName }

func (b nopBreaker) Allow() (Promise, error) {
	return nopPromise{}, nil
}

func (b nopBreaker) AllowCtx(context.Context) (Promise, error) {
	return nopPromise{}, nil
}

func (b nopBreaker) Do(req func() error) error {
	return req()
}

func (b nopBreaker) DoCtx(_ context.Context, req func() error) error {
	return req()
}

func (b nopBreaker) DoWithAcceptable(req func() error, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithAcceptableCtx(_ context.Context, req func() error, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithFallback(req func() error, _ Fallback) error {
	return req()
}

func (b nopBreaker) DoWithFallbackCtx(_ context.Context, req func() error, _ Fallback) error {
	return req()
}

func (b nopBreaker) DoWithFallbackAcceptable(req func() error, _ Fallback, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithFallbackAcceptableCtx(_ context.Context, req func() error,
	_ Fallback, _ Acceptable) error {
	return req()
}

// nopPromise 空实现 Promise，无需上报。
type nopPromise struct{}

func (nopPromise) Accept()       {}
func (nopPromise) Reject(string) {}
