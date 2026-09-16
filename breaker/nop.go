package breaker

import "context"

const nopBreakerName = "nopBreaker"

// nopBreaker is an implementation that never trips: requests always pass.
type nopBreaker struct{}

// NopBreaker returns a Breaker that never trips.
// Useful when breaker protection should be disabled for a call chain.
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

// nopPromise is an empty Promise implementation; nothing to report.
type nopPromise struct{}

func (nopPromise) Accept()       {}
func (nopPromise) Reject(string) {}
