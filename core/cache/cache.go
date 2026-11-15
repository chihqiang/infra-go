package cache

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("cache: key not found")

// Cache is the unified cache interface; the in-memory and the Redis
// implementations share the same set of methods.
//
// Note that the value type returned by Get/Take depends on the backend:
//   - MemCache returns the original type stored (Set(ctx,"k",5) reads back int(5);
//     Set(ctx,"k",u) reads back u itself)
//   - RedisCache has to serialize values, so it reads back the JSON decoding result
//     (Set(ctx,"k",5) reads back json.Number("5"); Set(ctx,"k",u) reads back map[string]any)
//
// Therefore code that type-asserts the value returned by Get (such as v.(int),
// v.(float64)) panics after switching backends.
// When you need a backend-agnostic read, use GetAs[T], which always round-trips
// through JSON and decodes into a concrete type:
//
//	n, err := cache.GetAs[int64](ctx, c, "counter")
type Cache interface {
	Get(ctx context.Context, key string) (any, error)
	Set(ctx context.Context, key string, value any) error
	SetEx(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Take(ctx context.Context, key string, fetch func() (any, error)) (any, error)
	// Increment adds delta to the numeric value of key; a missing key is initialized to delta.
	Increment(ctx context.Context, key string, delta int64) error
	// Decrement subtracts delta from the numeric value of key; a missing key is initialized to -delta.
	Decrement(ctx context.Context, key string, delta int64) error
	// Expire sets the time to live of key, after which it expires automatically;
	// a ttl <= 0 expires the key immediately.
	Expire(ctx context.Context, key string, ttl time.Duration) error
}
