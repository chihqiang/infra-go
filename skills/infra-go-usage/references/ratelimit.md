# ratelimit

Rate limiter implementations with two storage backends, in-memory and Redis: token
bucket, sliding window, concurrency limit, and chained limiting — freely interchangeable.

> The **HTTP rate limiting middleware** is not provided by this package: it now lives in the
> `httpx/middleware` subpackage (`middleware.NewRateLimit`), and is registered conveniently via
> `httpx.WithRateLimit` in the main httpx package. This package only provides the various
> `Limiter` types.

## Features

- **Dual storage backends**: in-memory (single node) and Redis (distributed), swappable
  through a unified `Limiter` interface
- **Token bucket**: tolerates burst traffic and refills tokens at a fixed rate
- **Sliding window**: precise control over the number of requests in a time window
- **Concurrency limit**: caps the number of in-flight requests (requires a manual `Release`)
- **Chained limiting**: chain several limiters, mixing in-memory and Redis
- **Lua scripts**: Redis limiters use Lua scripts to guarantee atomicity
- **Thread safety**: every limiter is safe for concurrent use

## Installation

```bash
go get github.com/chihqiang/infra-go/ratelimit
```

## Limiters

### Token bucket

Tokens are generated at a fixed rate and consumed by requests, allowing burst traffic:

```go
// --- In-memory ---
tb := ratelimit.NewTokenBucket(100, 200) // 100 QPS, burst 200

// --- Redis (shared across instances) ---
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
tb := ratelimit.NewRedisTokenBucket(rdb, "rate:tb", 100, 200)

// Usage
if tb.Allow() { /* request allowed */ }

// With context (the Redis variants support timeout/cancellation)
ok, err := tb.AllowContext(ctx)
```

### Sliding window

Allows at most N requests within a given time window:

```go
sw := ratelimit.NewSlidingWindow(100, time.Second)                    // in-memory: 100 per second
sw := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 100, time.Second) // Redis

if sw.Allow() { /* request allowed */ }
```

### Concurrency limit

Caps the number of requests handled concurrently; a manual `Release` is required:

```go
c := ratelimit.NewConcurrency(10) // at most 10 concurrent

if c.Allow() {
    defer c.Release()
    // handle the request
}
```

### Chained limiting

Chains several limiters (all of them must pass), mixing in-memory and Redis:

```go
memTB := ratelimit.NewTokenBucket(100, 200)
redisSW := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 1000, time.Minute)

chain := ratelimit.NewChain(memTB, redisSW)
if chain.Allow() { /* both limiters passed */ }
```

## HTTP rate limiting middleware

The HTTP rate limiting middleware has moved to the `httpx/middleware` subpackage:
`middleware.NewRateLimit(limiter, skipPaths...)` (object-oriented, returning a standard
`func(http.Handler) http.Handler` that gin/echo and others can reuse); the main httpx package
ships the `httpx.WithRateLimit` convenience registration. The `limiter` parameter type
`middleware.RateLimiter` has the same method set as this package's `Limiter`, so any limiter
here can be passed directly.

Features:

- Throttled requests get **429 Too Many Requests**, plus **`Retry-After`** per RFC 6585 §4
- Reuses the request context through `AllowContext`, so Redis limiters get timeout control
  automatically
- When the limiter itself fails, requests are **failed open** and an error is logged, so a
  flaky Redis cannot drag the service down
- `skipPaths` skips paths such as health checks (exact match or `*` prefix wildcard, same as
  `httpx.WithLogger`)
- A nil `limiter` degrades to no limiting and logs a warning (no panic)

### Retry-After (suggested retry interval)

Every limiter in this package implements `RetryAfter() time.Duration`, which the HTTP
middleware uses to build `Retry-After` (RFC 9110 §10.2.3), letting clients retry at the right
moment instead of backing off blindly:

| Limiter | Meaning of `RetryAfter()` |
|--------|------------------|
| `TokenBucket` | Time until the next token is available (derived from the live token count and `rate`) |
| `SlidingWindow` | When the oldest record slides out of the window |
| `RedisTokenBucket` | How long it takes to generate one token (estimated from `rate`) |
| `RedisSlidingWindow` | The whole window duration (a conservative upper bound, avoiding another Redis round trip on the throttling path) |
| `Concurrency` | **Not implemented** — the hold duration is unpredictable and any invented value would mislead clients |

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 2
```

> If a custom limiter does not implement `RetryAfter()`, use
> `middleware.NewRateLimit(lim).WithRetryAfter(d)` to supply a fixed value;
> when neither exists, **omit the header** rather than sending an invented duration.
>
> Values are always **rounded up to whole seconds**; anything under 1 second still emits `1`,
> avoiding `Retry-After: 0`.

### Usage with httpx

```go
server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))          // 100 QPS, burst 200
server.Use(httpx.WithRateLimit(ratelimit.NewSlidingWindow(10, time.Minute))) // 10 per minute
server.Use(httpx.WithRateLimit(redisLimiter, "/healthz", "/metrics"))        // skip probes
```

### Usage with standard net/http / other frameworks

```go
import "github.com/chihqiang/infra-go/httpx/middleware"

limiter := ratelimit.NewRedisTokenBucket(rdb, "api:limit", 100, 200)
handler := middleware.NewRateLimit(limiter).Middleware()(http.HandlerFunc(apiHandler))
http.ListenAndServe(":8080", handler)
```

### Per-user / per-IP fine-grained limiting

`httpx.WithRateLimit` is a global limiter (the whole service shares one instance). When you
need independent counters per user, IP, route, and so on, build a limiter from a dimension key
and wrap it yourself (get the client IP with `x.ClientIP`, which already handles reverse
proxies/XFF — do not hand-roll `net.SplitHostPort`):

```go
import "github.com/chihqiang/infra-go/httpx/x"

func RateLimitByIP(rdb *redis.Client, rate, burst float64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := x.ClientIP(r) // real client IP (resolves reverse proxies / trusted proxies)
            key := fmt.Sprintf("rate_limit:ip:%s", ip)
            limiter := ratelimit.NewRedisTokenBucket(rdb, key, rate, burst)
            if !limiter.Allow() {
                w.WriteHeader(http.StatusTooManyRequests)
                _, _ = w.Write([]byte(`{"code":429,"msg":"too many requests"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## Interface

All limiters implement `Limiter`, so the in-memory and Redis variants are interchangeable:

```go
type Limiter interface {
    Allow() bool
    AllowContext(ctx context.Context) (bool, error)
}
```

## Factory functions

Switch storage backends freely via `StoreType`:

```go
limiter := ratelimit.NewTokenBucketWithStore(
    ratelimit.StoreRedis,            // or ratelimit.StoreMemory
    rdb,                              // Redis client (nil for Memory)
    "rate_limit_key",                 // limiter key name (empty for Memory)
    ratelimit.TokenBucketConfig{Rate: 100, Burst: 200},
)

limiter = ratelimit.NewSlidingWindowWithStore(
    ratelimit.StoreRedis,
    rdb,
    "rate_limit_key",
    ratelimit.SlidingWindowConfig{Limit: 100, Window: time.Second},
)
```

## Distributed scenarios

Redis limiters suit multi-instance deployments: every instance shares the same Redis key to
enforce a global limit:

```go
// Instance 1 and instance 2 share one key → they share a 100 QPS quota
tb1 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)
tb2 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)
```

> **Requirement**: every instance must pass the **same key** to the limiter, and all instances
> must use the **same set** of `rate`/`burst`/`limit`/`window` parameters.
> With mismatched parameters the script judges by whatever the caller passed, which makes
> behavior unpredictable.

## How it works

### Redis token bucket

A Lua script plus a Redis Hash (`tokens` / `last_update`) guarantees atomicity: read the token
count and the last update time → compute the tokens added since → check whether enough are
available and consume them → update the state and the expiry.

### Redis sliding window

Implemented with a Lua script plus a Redis ZSET: `ZREMRANGEBYSCORE` drops records outside the
window → `ZCARD` counts the requests in the current window → if still under the limit, `ZADD`
the current timestamp → the key expiry is set for automatic cleanup.

The ZSET member format is `<process-unique prefix>:<millisecond timestamp>:<in-process count>`,
where the prefix is generated with `crypto/rand` to guarantee **cross-process** uniqueness.

> This matters for counting correctness: `ZADD` only updates the score of an existing member
> and does not increase the cardinality, while the window size is measured with `ZCARD`.
> If members carried no node identity, two instances starting at count 1 in the same
> millisecond would produce identical members → `ZADD` overwrites → `ZCARD` undercounts the
> real requests → more traffic is let through than `limit` allows, making the global limit
> useless.

## Error handling

```go
if !limiter.Allow() { /* rate limited */ }

ok, err := limiter.AllowContext(ctx)
if err != nil { /* Redis error or context cancellation */ }
if !ok { /* rate limited */ }
```

| Error | Description |
|------|------|
| `ErrLimitExceeded` | Defined but **currently returned by no limiter**; when throttled, `Allow`/`AllowContext` return `false` (with a nil error), and a non-nil error only appears when Redis or the ctx fails |
