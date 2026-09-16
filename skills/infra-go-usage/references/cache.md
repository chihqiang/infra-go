# cache

Unified cache interface offering two implementations — an **in-memory cache** (`MemCache`) and a **Redis distributed cache** (`RedisCache`) — so you can switch between local and distributed as needed.

## Features

- **Unified interface**: `Cache` is a non-generic interface; the in-memory and Redis implementations are identical, and one instance can hold values of any type (`any`)
- **Expiry deletion**: supports a default expiry and per-key expiry, with a slight jitter so many keys don't expire at the same moment (avalanche protection)
- **Cache stampede protection**: `Take` coalesces concurrent requests for the same key via SingleFlight, so the underlying fetch runs only once
- **Cache penetration protection** (Redis): when a lookup yields no result, a short-lived placeholder is written so nonexistent keys don't repeatedly hit the DB
- **LRU eviction** (in-memory): `WithLimit` caps the capacity and evicts the least recently used key once exceeded
- **Hit-rate statistics** (in-memory): periodically reports QPS, hit rate and element count (every minute by default)
- **Numeric increment/decrement**: `Increment` / `Decrement` provide atomic counting (mutex in memory, INCRBY in Redis), auto-initializing missing keys
- **Per-key TTL**: `Expire` sets a TTL on an existing key so it lapses automatically, without rewriting the whole cache entry

## Installation

```bash
go get github.com/chihqiang/infra-go/cache
```

## Unified interface

A miss returns `cache.ErrNotFound`; test it with the standard library `errors.Is(err, cache.ErrNotFound)` (wrapped errors are supported):

| Method | Description |
| ------ | ------ |
| `Get(ctx, key) (any, error)` | Return the value; returns `ErrNotFound` on a miss or after expiry |
| `Set(ctx, key, value any)` | Write using the default expiry |
| `SetEx(ctx, key, value any, ttl)` | Write with an explicit time-to-live `ttl`; `ttl <= 0` falls back to the default |
| `Delete(ctx, keys...)` | Delete one or more keys |
| `Take(ctx, key, fetch func()(any, error)) (any, error)` | Call `fetch` on a miss, store the result, and deduplicate concurrent calls to prevent stampedes |
| `Increment(ctx, key, delta)` | Increment the numeric value at key by `delta`; initializes to `delta` when absent |
| `Decrement(ctx, key, delta)` | Decrement the numeric value at key by `delta`; initializes to `-delta` when absent |
| `Expire(ctx, key, ttl)` | Set the time-to-live `ttl` for a key; expires afterwards; `ttl <= 0` expires it immediately; returns `ErrNotFound` when the key is absent |

> The in-memory implementation ignores `ctx`; the Redis implementation uses `ctx` for timeouts and cancellation.
> The interface uses non-generic `any`: callers don't need to instantiate a cache per value type.

### ⚠️ Get's return type differs between backends

`Get` / `Take` return `any`, but the two backends produce different Go types for the same value:

| Written | Read back from MemCache | Read back from RedisCache |
|------|--------------|----------------|
| `Set(ctx, "k", 5)` | `int(5)` | `json.Number("5")` |
| `Set(ctx, "k", u)` (struct) | `u` itself | `map[string]any` |
| `Set(ctx, "k", int64(2^53+1))` | preserved exactly | preserved exactly (`json.Number` keeps the literal losslessly) |

Consequently, code that **type-asserts directly on the value returned by `Get`** (e.g. `v.(int)`, `v.(float64)`) will panic after switching backends.
The Redis backend decodes with `json.Decoder.UseNumber()`, so numbers are always `json.Number`
(a string alias that preserves the original numeric literal; use `Int64()` / `Float64()` to get a value when needed).

For backend-independent reads, use `GetAs[T]` (which round-trips through JSON into a concrete type):

```go
n, err := cache.GetAs[int64](ctx, c, "counter")   // identical behavior on both backends
var u User
u, err = cache.GetAs[User](ctx, c, "user:1")
```

> Big-integer safety: `json.Number` preserves literals losslessly, so `int64` values above 2^53
> such as snowflake IDs or nanosecond timestamps don't lose precision on the Redis backend, and
> `GetAs[int64]` restores them exactly.
> If you use `Get` and get a `json.Number`, don't convert it to `float64` before converting to an integer.

## Counters and expiry

### Numeric increment / decrement

`Increment` / `Decrement` serve counter scenarios (page views, inventory, likes, etc.). A missing key is auto-initialized to `delta` / `-delta`; the in-memory implementation keeps the original value type and uses a lock for concurrency safety, while the Redis implementation uses the atomic `INCRBY` operation:

```go
_ = c.Increment(ctx, "visit:20260828", 1) // count +1; initialized to 1 when the key is absent
_ = c.Decrement(ctx, "stock:sku1", 3)     // stock -3; initialized to -3 when the key is absent
```

> Note: after incrementing on the Redis side, `Get` returns a `json.Number` (the established behavior of
> non-generic deserialization); use `GetAs[int64]` to get an `int64` back directly, or use the return
> value of `redisx.IncrBy` for exact integer arithmetic.

### Setting a time-to-live (Expire)

Sets a TTL on an **existing key** so it lapses automatically, without rewriting the whole cache entry (commonly used for renewal, temporary takedown, and similar cases):

```go
_ = c.Set(ctx, "config:v1", cfg)            // write first
_ = c.Expire(ctx, "config:v1", time.Hour)   // then make it expire after 1 hour
_ = c.Expire(ctx, "config:v1", 0)           // ttl <= 0: expire immediately (equivalent to delete)
// returns cache.ErrNotFound when the key is absent
```

> Precision difference: the in-memory implementation is millisecond-precision and applies jitter; Redis `EXPIRE` has second-level precision, and `Expire` passes `ttl` straight through without jitter (avoiding truncation to 0 seconds and immediate deletion).

## In-memory cache (MemCache)

```go
// the ctx is held by the cache instance and used to correlate background statistics logs with traces
c := cache.NewMemCache(ctx, time.Minute, cache.WithLimit(1000), cache.WithName("user"))
defer c.Close()

_ = c.Set(ctx, "key", "value")
v, err := c.Get(ctx, "key")
if err == nil {
    fmt.Println(v) // v is any; strings can be used directly
}
_ = c.Delete(ctx, "key")
```

| Option | Description |
| ------ | ------ |
| `WithLimit(limit)` | Capacity cap; LRU eviction once exceeded; `<= 0` means unlimited (default) |
| `WithName(name)` | Cache name, used to identify the statistics logs |

Extra methods (`MemCache` only): `Size()` returns the current element count; `Close()` stops the background statistics goroutine.

## Redis cache (RedisCache)

Values are serialized as JSON and stored in Redis; besides the common features it adds penetration protection and fast failure (does not fall through to the DB when Redis fails).

```go
rds := redisx.MustNew(redisx.Config{Addr: cfg.RedisAddr})
c := cache.NewRedisCache(rds, cache.WithExpire(time.Minute))
defer rds.Close()

u, err := c.Take(ctx, "user:1", func() (any, error) {
    return loadUserFromDB(1) // stampede protection: runs only once under concurrency
})
if err == nil {
    // Without generics the value comes back as map[string]any (numbers as json.Number);
    // use json.Marshal/Unmarshal to restore it into *User when needed
    var user *User
    _ = json.Unmarshal(mustMarshal(u), &user)
}
```

| Option | Description |
| ------ | ------ |
| `WithExpire(d)` | Default expiry; 7 days when unset |
| `WithNotFoundExpire(d)` | Expiry of the miss placeholder; 1 minute when unset |
| `WithCacheName(name)` | Cache name, used to identify logs |

**Penetration protection example**: when `fetch` returns `cache.ErrNotFound`, a short-lived placeholder is written, and for a while afterwards the same `Take` returns a miss directly instead of querying the DB again.

## Stampede protection (Take)

When several goroutines `Take` the same key concurrently, the underlying `fetch` runs only once and the result is shared with all callers:

```go
val, err := c.Take(ctx, "user:123", func() (any, error) {
    return loadUserFromDB(123) // runs only once under concurrency
})
if err != nil {
    // handle the error; a failed fetch is not written to the cache
}
```

## About types (non-generic)

The interface stores values as `any`, so callers don't need to instantiate a cache per type nor define generic type parameters. Handling the retrieved value:

- **In-memory cache (MemCache)**: `Set` stores the original object, so `Get`/`Take` return the same type that was stored and you can assert directly, e.g. `v.(*User)`.
- **Redis cache (RedisCache)**: values are stored as serialized JSON, and since `Get` cannot know the target type it always deserializes into `map[string]any` (numbers are `json.Number`, not `float64`); to get a concrete struct back, use `json.Marshal(got)` followed by `json.Unmarshal` into the target type, or use `GetAs[T]` directly.
- Scalars (string/int/bool, etc.) can be used as returned, with no extra handling.

> The differences above mean that **code which type-asserts the result of `Get` is not portable across backends**.
> If the same business code needs to switch between the two backends, use `GetAs[T]` consistently:

```go
// backend-independent: both backends return consistent Go types
n, err := cache.GetAs[int](ctx, c, "count")
u, err := cache.GetAs[User](ctx, c, "user:1")
tags, err := cache.GetAs[[]string](ctx, c, "tags")

// a miss returns (zero value, cache.ErrNotFound)
// a type mismatch returns an error instead of silently yielding a zero value
```

> If you want the Redis cache to return a concrete type directly, consider a fill-style API
> (such as `Get(ctx, key, &user)`); this package doesn't provide one yet, but it can be extended as needed.

## Notes

- When `Take`'s `fetch` returns an error nothing is written to the cache and the error is returned as-is; in the Redis implementation an `ErrNotFound` from fetch writes a placeholder
- The in-memory cache is shared **within a single process** only; use `RedisCache` for cross-instance sharing
- The effective expiry is randomized within `[0.95, 1.05] * expire`, avoiding simultaneous expiry
- `Expire` (Redis) passes `ttl` straight through (EXPIRE has second-level precision) with no jitter; the in-memory implementation applies jitter
- Every method of the in-memory implementation is concurrency-safe; the instance must not be used after `Close`
