package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/redisx"
	"github.com/chihqiang/infra-go/syncx"
)

// Default configuration.
const (
	// Default expiry: 7 days, which keeps caches in Redis from piling up forever.
	defaultRedisExpiry = time.Hour * 24 * 7
	// Default not-found placeholder expiry: 1 minute, so that a hot missing key does
	// not penetrate to the DB over and over.
	defaultRedisNotFoundExpiry = time.Minute
	// Default cache name, used to identify the cache in logs.
	defaultRedisCacheName = "redis_cache"
)

// notFoundPlaceholder is the not-found placeholder used to prevent cache penetration.
const notFoundPlaceholder = "*"

// errPlaceholder is an internal error: the not-found placeholder was hit.
// It differs from ErrNotFound in that the placeholder means "this key is confirmed
// absent", so Take will not penetrate to the DB again when it hits the placeholder.
var errPlaceholder = errors.New("cache: placeholder")

// RedisCache is the Redis implementation of the Cache interface.
//
// Features:
//   - Values are stored as JSON
//   - Stampede protection: Take merges concurrent requests for the same key via
//     SingleFlight
//   - Penetration protection: a short-lived placeholder is written when a query
//     returns nothing
//   - Avalanche protection: expiries carry a slight jitter
//   - Fail fast: a Redis failure does not penetrate to the DB
//
// See MemCache for the in-memory implementation. Both implement the Cache interface,
// so you can switch between local and distributed caching.
type RedisCache struct {
	rds            *redisx.Client
	expiry         time.Duration
	notFoundExpiry time.Duration
	barrier        *syncx.SingleFlight[any]
	unstable       *unstable
	name           string
}

// RedisCacheOption customizes the behaviour of the Redis cache.
type RedisCacheOption func(*redisCacheOptions)

// redisCacheOptions holds the internal options of the Redis cache.
type redisCacheOptions struct {
	expiry         time.Duration // default expiry
	notFoundExpiry time.Duration // expiry of the not-found placeholder
	name           string        // cache name, used to identify it in logs
}

// WithExpire sets the default expiry.
// It defaults to 7 days when unset.
func WithExpire(d time.Duration) RedisCacheOption {
	return func(o *redisCacheOptions) { o.expiry = d }
}

// WithNotFoundExpire sets the expiry of the not-found placeholder.
// The placeholder prevents cache penetration: a query with no result briefly caches
// a "not found" mark. It defaults to 1 minute when unset.
func WithNotFoundExpire(d time.Duration) RedisCacheOption {
	return func(o *redisCacheOptions) { o.notFoundExpiry = d }
}

// WithCacheName sets the cache name, used to identify the cache in logs.
func WithCacheName(name string) RedisCacheOption {
	return func(o *redisCacheOptions) { o.name = name }
}

// NewRedisCache creates and returns a Redis-backed cache instance.
// rds is the redisx client; the options customize the default expiry, the placeholder
// expiry, the name, and so on.
//
//	var c cache.Cache = cache.NewRedisCache(rds, cache.WithExpire(time.Minute))
func NewRedisCache(rds *redisx.Client, opts ...RedisCacheOption) *RedisCache {
	var o redisCacheOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.expiry <= 0 {
		o.expiry = defaultRedisExpiry
	}
	if o.notFoundExpiry <= 0 {
		o.notFoundExpiry = defaultRedisNotFoundExpiry
	}
	if o.name == "" {
		o.name = defaultRedisCacheName
	}

	return &RedisCache{
		rds:            rds,
		expiry:         o.expiry,
		notFoundExpiry: o.notFoundExpiry,
		barrier:        syncx.NewSingleFlight[any](),
		unstable:       newUnstable(expiryDeviation),
		name:           o.name,
	}
}

// Get returns the value of key; a miss or a placeholder hit returns ErrNotFound.
func (c *RedisCache) Get(ctx context.Context, key string) (any, error) {
	v, err := c.doGet(ctx, key)
	if errors.Is(err, errPlaceholder) {
		return nil, ErrNotFound
	}
	return v, err
}

// Set writes value to the cache using the default expiry.
func (c *RedisCache) Set(ctx context.Context, key string, value any) error {
	return c.SetEx(ctx, key, value, c.expiry)
}

// SetEx writes value to the cache with the given time to live ttl.
func (c *RedisCache) SetEx(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.expiry
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.rds.Set(ctx, key, string(data), c.aroundDuration(ttl))
}

// Delete removes one or more keys.
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := c.rds.Del(ctx, keys...)
	return err
}

// Take returns the value of key; on a miss it calls fetch and writes the result to
// the cache.
// Stampede protection: concurrent calls for the same key run fetch only once.
// Penetration protection: when fetch returns ErrNotFound a short-lived placeholder is
// written. Fail fast: a Redis failure does not penetrate to the DB.
func (c *RedisCache) Take(ctx context.Context, key string, fetch func() (any, error)) (any, error) {
	val, err := c.barrier.Do(key, func() (any, error) {
		// Double check: another concurrent request may have written it while waiting
		v, e := c.doGet(ctx, key)
		if e == nil {
			return v, nil
		}
		if errors.Is(e, errPlaceholder) {
			// Placeholder hit: the key is confirmed absent, so return a miss and do not
			// penetrate to the DB
			return nil, ErrNotFound
		}
		if !errors.Is(e, ErrNotFound) {
			// Redis failure: fail fast instead of penetrating the request to the DB
			return nil, e
		}

		v, e = fetch()
		if e != nil {
			if errors.Is(e, ErrNotFound) {
				// The DB has no data either: write a short-lived placeholder to prevent
				// cache penetration
				if err := c.setNotFound(ctx, key); err != nil {
					logger.InfofCtx(ctx, "cache(%s): set not found placeholder failed, key: %s, error: %v",
						c.name, key, err)
				}
				return nil, ErrNotFound
			}
			return nil, e
		}

		// A failed cache write does not affect the main flow (consistent with the
		// stampede semantics), it is only logged
		if err := c.SetEx(ctx, key, v, c.expiry); err != nil {
			logger.InfofCtx(ctx, "cache(%s): set cache failed, key: %s, error: %v", c.name, key, err)
		}
		return v, nil
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Increment adds delta to the numeric value of key; a missing key is initialized to
// delta. It uses the atomic Redis INCRBY command underneath.
func (c *RedisCache) Increment(ctx context.Context, key string, delta int64) error {
	_, err := c.rds.IncrBy(ctx, key, delta)
	return err
}

// Decrement subtracts delta from the numeric value of key; a missing key is
// initialized to -delta. It uses the atomic Redis INCRBY command with a negative
// value underneath.
func (c *RedisCache) Decrement(ctx context.Context, key string, delta int64) error {
	_, err := c.rds.IncrBy(ctx, key, -delta)
	return err
}

// Expire sets the time to live of key; the key expires automatically afterwards.
// ttl <= 0 expires it immediately (the key is deleted); a missing key returns
// ErrNotFound.
// Note: Redis EXPIRE has second-level precision, so ttl is passed through without
// jitter (which would be truncated to 0 seconds and delete the key immediately).
func (c *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if ttl <= 0 {
		_, err := c.rds.Del(ctx, key)
		return err
	}
	ok, err := c.rds.Expire(ctx, key, ttl)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// doGet performs the internal read, distinguishing a miss (ErrNotFound) from a
// placeholder hit (errPlaceholder).
func (c *RedisCache) doGet(ctx context.Context, key string) (any, error) {
	data, err := c.rds.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redisx.ErrNil) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if data == notFoundPlaceholder {
		return nil, errPlaceholder
	}

	// Without generics the target type is unknown at compile time, so everything is
	// deserialized into any. Decoding enables UseNumber (see decodeUseNumber) so that
	// JSON numbers stay json.Number instead of float64, keeping large int64 integers
	// (> 2^53) from losing precision in a float64 round-trip. For complex types such as
	// structs or pointers the caller has to type-assert or convert the result itself
	// (see cache.md).
	var v any
	if err := decodeUseNumber(data, &v); err != nil {
		// Deserialization failed: return a miss so the caller reloads, but do NOT delete
		// the key.
		//
		// It must not be deleted: the same key may have been written in a non-JSON format
		// by another component (e.g. a raw string stored with redisx.Client.Set).
		// Deleting it automatically would cause the subtle failure of "the other side
		// just wrote it and this cache deleted it", which is hard to diagnose. If dirty
		// data really needs to be cleaned up, the caller should decide and delete it
		// explicitly with Delete.
		logger.InfofCtx(ctx, "cache(%s): unmarshal cache failed, key: %s, error: %v", c.name, key, err)
		return nil, ErrNotFound
	}
	return v, nil
}

// setNotFound writes the not-found placeholder with a jittered expiry.
func (c *RedisCache) setNotFound(ctx context.Context, key string) error {
	_, err := c.rds.SetNX(ctx, key, notFoundPlaceholder, c.aroundDuration(c.notFoundExpiry))
	return err
}

// aroundDuration returns a jittered expiry, preventing a large number of keys from
// expiring at the same instant (avalanche protection).
func (c *RedisCache) aroundDuration(d time.Duration) time.Duration {
	return c.unstable.AroundDuration(d)
}

// decodeUseNumber decodes JSON text into out, keeping numbers as json.Number instead
// of float64.
//
// Why not use json.Unmarshal directly: it decodes every JSON number into a float64.
// A float64 has only a 53-bit significand, so large int64 integers (> 2^53) such as
// snowflake IDs or nanosecond timestamps lose precision already at the Get stage,
// which GetAs cannot repair. json.Number is a string alias that preserves the original
// numeric literal losslessly; Int64/Float64 convert it to a concrete numeric type when
// needed, and json.Marshal writes it back verbatim (without quotes).
//
// Strictness matches json.Unmarshal: trailing garbage content returns an error too.
func decodeUseNumber(data string, out *any) error {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("cache: unexpected trailing data in cached value")
	}
	return nil
}
