package breaker

import (
	"context"
	"sync"
)

var (
	breakersLock sync.RWMutex
	breakers     = make(map[string]Breaker)
)

// GetBreaker returns the breaker registered under name, creating and caching it
// if it does not exist yet.
// Breakers with the same name are shared globally, which keeps the statistics of
// the same target (downstream service or endpoint) consistent.
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

// NoBreakerFor registers a non-tripping implementation under name (disabling
// breaker protection).
func NoBreakerFor(name string) {
	breakersLock.Lock()
	breakers[name] = NopBreaker()
	breakersLock.Unlock()
}

// RegistrySize returns the number of breakers cached in the global registry.
//
// Mainly useful for observability and tests: the registry caches by name forever
// and never evicts, so callers must keep the cardinality of name bounded.
// If this value keeps growing with request volume, a high-cardinality field has
// leaked into name (for example using the concrete path /users/123 instead of the
// route template /users/{id}).
func RegistrySize() int {
	breakersLock.RLock()
	defer breakersLock.RUnlock()
	return len(breakers)
}

// RemoveBreaker removes the breaker registered under name from the global
// registry (it is recreated on the next lookup).
// Use it to release names that are no longer needed, so the registry does not
// grow forever.
// Callers that already hold the instance are unaffected (they keep using the old
// instance).
func RemoveBreaker(name string) {
	breakersLock.Lock()
	delete(breakers, name)
	breakersLock.Unlock()
}

// Do runs the request through the breaker registered under name.
func Do(name string, req func() error) error {
	return GetBreaker(name).Do(req)
}

// DoCtx is like Do, with context support.
func DoCtx(ctx context.Context, name string, req func() error) error {
	return GetBreaker(name).DoCtx(ctx, req)
}

// DoWithAcceptable runs the request through the breaker registered under name,
// with a custom error-accepting policy.
func DoWithAcceptable(name string, req func() error, acceptable Acceptable) error {
	return GetBreaker(name).DoWithAcceptable(req, acceptable)
}

// DoWithAcceptableCtx is like DoWithAcceptable, with context support.
func DoWithAcceptableCtx(ctx context.Context, name string, req func() error,
	acceptable Acceptable) error {
	return GetBreaker(name).DoWithAcceptableCtx(ctx, req, acceptable)
}

// DoWithFallback runs the request through the breaker registered under name, and
// runs fallback when the breaker is open.
func DoWithFallback(name string, req func() error, fallback Fallback) error {
	return GetBreaker(name).DoWithFallback(req, fallback)
}

// DoWithFallbackCtx is like DoWithFallback, with context support.
func DoWithFallbackCtx(ctx context.Context, name string, req func() error,
	fallback Fallback) error {
	return GetBreaker(name).DoWithFallbackCtx(ctx, req, fallback)
}

// DoWithFallbackAcceptable runs the request through the breaker registered under
// name, with fallback and a custom error-accepting policy.
func DoWithFallbackAcceptable(name string, req func() error, fallback Fallback,
	acceptable Acceptable) error {
	return GetBreaker(name).DoWithFallbackAcceptable(req, fallback, acceptable)
}

// DoWithFallbackAcceptableCtx is like DoWithFallbackAcceptable, with context support.
func DoWithFallbackAcceptableCtx(ctx context.Context, name string, req func() error,
	fallback Fallback, acceptable Acceptable) error {
	return GetBreaker(name).DoWithFallbackAcceptableCtx(ctx, req, fallback, acceptable)
}
