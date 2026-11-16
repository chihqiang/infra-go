# ratelimit

Rate limiter implementations with two storage backends, in-memory and Redis: token
bucket, sliding window, concurrency limit, and chained limiting — freely interchangeable.

> The **HTTP rate limiting middleware** lives in `rest/handler` as
> `resthandler.NewRateLimitHandler`, and is registered conveniently via `rest.WithRateLimit`.
> This package only provides the `Limiter` types.

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
go get github.com/chihqiang/infra-go/core/ratelimit
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

`rest/handler.NewRateLimitHandler(RateLimitConfig{Enabled: true, Limiter: limiter})` wraps any
`Limiter` from this package. The server can mount it from its middleware config, which keeps the
dependency in one place:

```go
import resthandler "github.com/chihqiang/infra-go/rest/handler"

conf := rest.DefaultConfig()
conf.Middlewares.RateLimit = resthandler.RateLimitConfig{
    Enabled: true,
    Limiter: ratelimit.NewTokenBucket(100, 200),
}
server, _ := rest.NewServer(conf)

// or mount it manually to control ordering
server.Use(resthandler.NewRateLimitHandler(resthandler.RateLimitConfig{
	Enabled: true,
	Limiter: ratelimit.NewSlidingWindow(10, time.Minute),
}).Handle)
```

Behaviour:

- Throttled requests get **429 Too Many Requests**
- **`Retry-After`** is set when the limiter implements `RetryAfter() time.Duration` (all
  limiters in this package do); values are rounded up to whole seconds and never below `1`.
  Limiters without a hint omit the header instead of inventing a value
- The limiter is called through `AllowContext`, so Redis limiters get request-context control
- A limiter **error** produces **503 Service Unavailable** and skips the handler; use
  `NewMaxConnsHandler` alongside it if you prefer capacity-based rejection
- A nil limiter disables the middleware entirely (no panic)

| Limiter | Meaning of `RetryAfter()` |
|--------|------------------|
| `TokenBucket` | Time until the next token is available |
| `SlidingWindow` | When the oldest record slides out of the window |
| `RedisTokenBucket` | How long it takes to generate one token |
| `RedisSlidingWindow` | The whole window duration (conservative, avoids another round trip) |
| `Concurrency` | **Not implemented** — the hold duration is unpredictable |

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 2
```

### Usage with a plain net/http handler

```go
limiter := ratelimit.NewTokenBucket(100, 200)
handler := resthandler.NewRateLimitHandler(resthandler.RateLimitConfig{
	Enabled: true,
	Limiter: limiter,
}).Handle(apiHandler)
http.ListenAndServe(":8080", handler)
```

### Per-user / per-IP fine-grained limiting

`resthandler.NewRateLimitHandler` is a global limiter (the whole service shares one instance). When you
need independent counters per user, IP, route, and so on, build a limiter from a dimension key
and wrap it yourself (get the client IP with `rest.ClientIP`, which already handles reverse
proxies/XFF — do not hand-roll `net.SplitHostPort`):

```go
func RateLimitByIP(rdb redis.UniversalClient, rate, burst float64) chain.Middleware {
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            key := fmt.Sprintf("rate_limit:ip:%s", rest.ClientIP(r))
            limiter := ratelimit.NewRedisTokenBucket(rdb, key, rate, burst)
            if allowed, _ := limiter.AllowContext(r.Context()); !allowed {
                _ = rest.WriteJSON(w, http.StatusTooManyRequests, rest.WrapBaseResponse(rest.ErrTooManyRequests))
                return
            }
            next(w, r)
        }
    }
}
```

`rest.ClientIP` already resolves reverse proxies / XFF — do not hand-roll `net.SplitHostPort`.

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

## Invalid configuration

`NewTokenBucket` / `NewSlidingWindow` (and their Redis variants) clamp negative
`rate`, `burst` and `limit` values instead of panicking: a negative limit becomes
`0` (deny every request) and a negative rate/capacity behaves like `0`. A zero
window lets records leave the window immediately.
