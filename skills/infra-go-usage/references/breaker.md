# breaker

Circuit breaker based on Google's SRE adaptive overload algorithm; protects downstream dependencies (databases, HTTP APIs, Redis, etc.) from cascading failures.

## Features

- **Google SRE algorithm**: computes the error rate over a sliding window and rejects requests probabilistically to avoid avalanches
- **Automatic three-state transitions**: closed → open → half-open (probe requests are allowed through after the cooldown; success restores the circuit)
- **Fail fast**: while the circuit is open, requests immediately return `ErrServiceUnavailable` instead of waiting for a slow downstream
- **Custom error policy**: `DoWithAcceptable` excludes business errors such as 4xx, sensing downstream health precisely
- **Fallback support**: `DoWithFallback` runs fallback logic (cache, queue, friendly error) while the circuit is open
- **Promise mode**: `Allow` gives manual control over success/failure reporting
- **Global management**: share breakers by name; the `Do` helpers work out of the box

## Installation

```bash
go get github.com/chihqiang/infra-go/breaker
```

## Basic usage

```go
b := breaker.NewBreaker(breaker.WithName("payment-gateway"))

// Simple mode: count every non-nil error
err := b.Do(func() error {
    return callPaymentAPI(req)
})
if errors.Is(err, breaker.ErrServiceUnavailable) {
    // Circuit is open, downstream unavailable
}
```

## Fallback (DoWithFallback)

Runs fallback logic while the circuit is open:

```go
err := b.DoWithFallback(func() error {
    return callPaymentAPI(req)
}, func(err error) error {
    // Return a cached result, enqueue for retry, or return a friendly error
    return serveCachedResult(req)
})
```

## Excluding business errors (DoWithAcceptable)

Precisely control which errors count as failures, so business errors such as `ErrNotFound` don't trip the breaker:

```go
err := b.DoWithAcceptable(func() error {
    return callPaymentAPI(req)
}, func(err error) bool {
    // true = "this error is acceptable, don't count it toward the breaker"
    return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized)
})
```

Use `DoWithFallbackAcceptable` to combine fallback and a custom error policy.

## Promise mode (manual control)

`Allow` suits scenarios that need to report results asynchronously:

```go
promise, err := b.Allow()
if err != nil {
    // Circuit is open
    return err
}
// Report the result after executing the request
if success {
    promise.Accept()
} else {
    promise.Reject("upstream timeout")
}
```

## Global manager

Share breakers by name; useful when the same downstream is called from many places (consistent statistics):

```go
// Breakers with the same name are shared globally
err := breaker.Do("payment-gateway", func() error {
    return callPaymentAPI(req)
})

// Disable breaker protection for one call chain
breaker.NoBreakerFor("health-check")
```

## Context variants

Every `Do` method has a `DoCtx` variant: when the context is cancelled it returns the context error directly without executing the request:

```go
err := b.DoCtx(ctx, func() error {
    return callPaymentAPI(req)
})
```

## Method overview

| Method | Description |
| ------ | ------ |
| `Allow()` / `AllowCtx(ctx)` | Check whether the request is allowed; returns a Promise |
| `Do(req)` / `DoCtx(ctx, req)` | Execute the request; fails immediately while the circuit is open |
| `DoWithAcceptable(req, acceptable)` | Custom error-acceptance policy |
| `DoWithFallback(req, fallback)` | Run fallback while the circuit is open |
| `DoWithFallbackAcceptable(req, fallback, acceptable)` | Fallback + custom error policy |
| `GetBreaker(name)` / `Do(name, req)` | Globally shared breakers |

## Notes

- **Don't swallow errors**: the breaker relies on accurate error feedback to compute the error rate; don't swallow real errors in the callback
- **Use it together with timeouts**: the breaker prevents cascading failures, but every call still needs a timeout to avoid goroutine buildup
- **Name your breakers**: a unique name lets logs distinguish different failure sources
- When the circuit opens, a warning log is emitted listing the last 5 failure reasons, making troubleshooting easier
