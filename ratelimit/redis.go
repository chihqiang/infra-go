package ratelimit

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// memberPrefix is a process-unique prefix used to build the ZSET members of the Redis
// sliding window.
//
// Members must be unique **across processes**: the sliding window counts the requests in the
// window with ZCARD, while ZADD only updates the score of an existing member and does not
// increase the cardinality. The old implementation used `<millisecond>:<in-process atomic
// counter>` as the member, so two processes that both produced counter=1 in the same
// millisecond generated identical members -> ZADD overwrote them -> ZCARD underestimated the
// real number of requests -> more requests were let through than limit allows (measured:
// with limit=2, two requests still left ZCARD at 1).
//
// Members therefore carry a 64-bit random prefix (crypto/rand); when the random source is
// unavailable it falls back to hostname + pid + start time in nanoseconds, which still
// guarantees uniqueness across processes.
var memberPrefix = newMemberPrefix()

func newMemberPrefix() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	host, _ := os.Hostname()
	return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
}

// memberCounter produces the in-process unique suffix of the ZSET members written by the
// Redis sliding window. Combined with memberPrefix it guarantees that concurrent requests in
// the same process never generate the same member.
var memberCounter uint64

// RedisClient is the Redis client interface.
// It is compatible with *redis.Client, *redis.ClusterClient and *redis.Ring.
type RedisClient = redis.UniversalClient

// --- Redis token bucket ---

// RedisTokenBucket is a Redis-based distributed token bucket rate limiter.
// A Lua script provides atomicity, which suits multi-instance deployments.
type RedisTokenBucket struct {
	client RedisClient
	key    string
	rate   float64 // tokens generated per second
	burst  float64 // bucket capacity
}

// NewRedisTokenBucket creates a Redis token bucket rate limiter.
// client is the Redis client and key is the rate limit key (it should be globally unique).
// rate is the number of tokens generated per second and burst is the bucket capacity.
func NewRedisTokenBucket(client RedisClient, key string, rate, burst float64) *RedisTokenBucket {
	return &RedisTokenBucket{
		client: client,
		key:    key,
		rate:   rate,
		burst:  burst,
	}
}

// tokenBucketScript is the token bucket Lua script.
// Args: KEYS[1]=key, ARGV[1]=rate, ARGV[2]=burst, ARGV[3]=current timestamp (seconds)
// Returns: 1=allowed, 0=rate limited
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local data = redis.call('hmget', key, 'tokens', 'last_update')
local tokens = tonumber(data[1]) or burst
local last_update = tonumber(data[2]) or now

-- Tokens generated since the last update
local elapsed = math.max(0, now - last_update)
tokens = math.min(burst, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
    tokens = tokens - 1
    allowed = 1
end

-- Persist the state
redis.call('hmset', key, 'tokens', tokens, 'last_update', now)
-- Expire the key after the time needed to refill the bucket, plus 1 second
local ttl = math.ceil(burst / rate) + 1
redis.call('expire', key, ttl)

return allowed
`)

// Allow reports whether the request is allowed.
func (tb *RedisTokenBucket) Allow() bool {
	ok, _ := tb.AllowContext(context.Background())
	return ok
}

// RetryAfter returns the suggested retry delay: the time until the next token is available.
//
// It estimates an upper bound from the configured rate (without an extra Redis round trip):
// the server-side script maintains the token state, so the client cannot learn the live
// remainder without cost, and "the time needed to generate one token" is exactly when a rate
// limited request can first succeed.
// It implements the http-layer Retry-After semantics (RFC 9110 §10.2.3).
func (tb *RedisTokenBucket) RetryAfter() time.Duration {
	if tb.rate <= 0 {
		return 0
	}
	wait := time.Duration(float64(time.Second) / tb.rate)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return wait
}

// AllowContext is the context-aware check.
func (tb *RedisTokenBucket) AllowContext(ctx context.Context) (bool, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	result, err := tokenBucketScript.Run(ctx, tb.client, []string{tb.key},
		tb.rate, tb.burst, now).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis token bucket error: %w", err)
	}
	return result == 1, nil
}

// --- Redis sliding window ---

// RedisSlidingWindow is a Redis-based distributed sliding window rate limiter.
// It is built on a sorted set (ZSET) and suits multi-instance deployments.
type RedisSlidingWindow struct {
	client RedisClient
	key    string
	limit  int
	window time.Duration
}

// NewRedisSlidingWindow creates a Redis sliding window rate limiter.
// client is the Redis client and key is the rate limit key (it should be globally unique).
// limit is the maximum number of requests within the window and window is the window size.
func NewRedisSlidingWindow(client RedisClient, key string, limit int, window time.Duration) *RedisSlidingWindow {
	return &RedisSlidingWindow{
		client: client,
		key:    key,
		limit:  limit,
		window: window,
	}
}

// slidingWindowScript is the sliding window Lua script.
// Args: KEYS[1]=key, ARGV[1]=current timestamp (ms), ARGV[2]=window size (ms), ARGV[3]=limit,
// ARGV[4]=unique identifier
// Returns: 1=allowed, 0=rate limited
var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

-- Remove the records that have left the window
local cutoff = now - window_ms
redis.call('zremrangebyscore', key, '-inf', cutoff)

-- Count the requests in the current window
local count = redis.call('zcard', key)

if count < limit then
    -- Add the current request
    redis.call('zadd', key, now, member)
    -- Set the expiry
    redis.call('pexpire', key, window_ms + 1000)
    return 1
else
    return 0
end
`)

// Allow reports whether the request is allowed.
func (sw *RedisSlidingWindow) Allow() bool {
	ok, _ := sw.AllowContext(context.Background())
	return ok
}

// RetryAfter returns the suggested retry delay: the longest time needed for the whole window
// to slide past.
//
// Unlike the in-memory implementation, the Redis side cannot know the timestamp of the oldest
// record locally (that needs an extra Redis round trip, and this is the rate limiting path
// where extra load should be avoided), so it reports a conservative upper bound: after
// waiting for a full window, quota is guaranteed to be available.
// It implements the http-layer Retry-After semantics (RFC 9110 §10.2.3).
func (sw *RedisSlidingWindow) RetryAfter() time.Duration {
	if sw.window <= 0 {
		return 0
	}
	return sw.window
}

// AllowContext is the context-aware check.
func (sw *RedisSlidingWindow) AllowContext(ctx context.Context) (bool, error) {
	now := time.Now().UnixMilli()
	// Member format: <process-unique prefix>:<millisecond timestamp>:<in-process counter>
	// The prefix guarantees uniqueness across processes and the counter guarantees
	// uniqueness across concurrent requests in the same process; without either one ZADD
	// overwrites an existing member, so ZCARD underestimates the requests in the window and
	// the limit can be bypassed.
	counter := atomic.AddUint64(&memberCounter, 1)
	member := memberPrefix + ":" + strconv.FormatInt(now, 10) + ":" + strconv.FormatUint(counter, 10)

	result, err := slidingWindowScript.Run(ctx, sw.client, []string{sw.key},
		now, sw.window.Milliseconds(), sw.limit, member).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis sliding window error: %w", err)
	}
	return result == 1, nil
}

// --- Factory functions ---

// StoreType is the rate limiter storage type.
type StoreType string

const (
	// StoreMemory is in-memory storage (single node).
	StoreMemory StoreType = "memory"
	// StoreRedis is Redis storage (distributed).
	StoreRedis StoreType = "redis"
)

// TokenBucketConfig is the token bucket configuration.
type TokenBucketConfig struct {
	Rate  float64 // tokens generated per second
	Burst float64 // bucket capacity
}

// SlidingWindowConfig is the sliding window configuration.
type SlidingWindowConfig struct {
	Limit  int           // maximum number of requests within the window
	Window time.Duration // window size
}

// NewTokenBucketWithStore creates a token bucket rate limiter for the given storage type.
// store is the storage type and client is the Redis client (it must be non-nil for
// StoreRedis).
// key is the rate limit key (used when store is StoreRedis).
func NewTokenBucketWithStore(store StoreType, client RedisClient, key string, cfg TokenBucketConfig) Limiter {
	switch store {
	case StoreRedis:
		return NewRedisTokenBucket(client, key, cfg.Rate, cfg.Burst)
	default:
		return NewTokenBucket(cfg.Rate, cfg.Burst)
	}
}

// NewSlidingWindowWithStore creates a sliding window rate limiter for the given storage type.
// store is the storage type and client is the Redis client (it must be non-nil for
// StoreRedis).
// key is the rate limit key (used when store is StoreRedis).
func NewSlidingWindowWithStore(store StoreType, client RedisClient, key string, cfg SlidingWindowConfig) Limiter {
	switch store {
	case StoreRedis:
		return NewRedisSlidingWindow(client, key, cfg.Limit, cfg.Window)
	default:
		return NewSlidingWindow(cfg.Limit, cfg.Window)
	}
}
