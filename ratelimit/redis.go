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

// memberPrefix 是进程级唯一前缀，用于生成 Redis 滑动窗口的 ZSET 成员。
//
// 成员必须**跨进程**唯一：滑动窗口用 ZCARD 统计窗口内请求数，而 ZADD 对已存在
// 的成员只更新 score、不增加基数。旧实现用 `<毫秒>:<进程内原子计数>` 作为成员，
// 两个进程在同一毫秒各自产生 counter=1 时成员完全相同 → ZADD 覆盖 → ZCARD 低估
// 真实请求数 → 实际放行量超过 limit（实测：limit=2 时 2 次请求 ZCARD 仍为 1）。
//
// 因此成员包含 64 位随机前缀（crypto/rand）；随机源不可用时退化为
// 主机名 + pid + 启动纳秒，仍可保证跨进程不重复。
var memberPrefix = newMemberPrefix()

func newMemberPrefix() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	host, _ := os.Hostname()
	return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
}

// memberCounter 用于生成 Redis 滑动窗口中 ZSET 成员的进程内唯一后缀。
// 与 memberPrefix 组合，保证同进程并发请求不会生成相同成员。
var memberCounter uint64

// RedisClient Redis 客户端接口。
// 兼容 *redis.Client、*redis.ClusterClient 和 *redis.Ring。
type RedisClient = redis.UniversalClient

// --- Redis 令牌桶 ---

// RedisTokenBucket 基于 Redis 的分布式令牌桶限流器。
// 使用 Lua 脚本保证原子性，适用于多实例部署场景。
type RedisTokenBucket struct {
	client RedisClient
	key    string
	rate   float64 // 每秒生成令牌数
	burst  float64 // 桶容量
}

// NewRedisTokenBucket 创建 Redis 令牌桶限流器。
// client 为 Redis 客户端，key 为限流键名（应全局唯一）。
// rate 为每秒生成的令牌数，burst 为桶容量。
func NewRedisTokenBucket(client RedisClient, key string, rate, burst float64) *RedisTokenBucket {
	return &RedisTokenBucket{
		client: client,
		key:    key,
		rate:   rate,
		burst:  burst,
	}
}

// tokenBucketScript 令牌桶 Lua 脚本。
// 参数：KEYS[1]=键名, ARGV[1]=rate, ARGV[2]=burst, ARGV[3]=当前时间戳(秒)
// 返回：1=允许, 0=限流
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local data = redis.call('hmget', key, 'tokens', 'last_update')
local tokens = tonumber(data[1]) or burst
local last_update = tonumber(data[2]) or now

-- 计算自上次更新以来生成的令牌
local elapsed = math.max(0, now - last_update)
tokens = math.min(burst, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
    tokens = tokens - 1
    allowed = 1
end

-- 保存状态
redis.call('hmset', key, 'tokens', tokens, 'last_update', now)
-- 设置过期时间为令牌桶填满所需时间 + 1 秒
local ttl = math.ceil(burst / rate) + 1
redis.call('expire', key, ttl)

return allowed
`)

// Allow 检查是否允许请求通过。
func (tb *RedisTokenBucket) Allow() bool {
	ok, _ := tb.AllowContext(context.Background())
	return ok
}

// RetryAfter 返回建议的重试等待时间：距下一个令牌可用的时长。
//
// 基于配置的 rate 给出上界估计（不额外访问 Redis）：
// 桶内令牌状态由服务端脚本维护，客户端无法无开销地得知实时余量，
// 而“令牌生成一个所需的时间”正是被限流后最早可能成功的时机。
// 实现 http 层的 Retry-After 语义（RFC 9110 §10.2.3）。
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

// AllowContext 带 context 的检查。
func (tb *RedisTokenBucket) AllowContext(ctx context.Context) (bool, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	result, err := tokenBucketScript.Run(ctx, tb.client, []string{tb.key},
		tb.rate, tb.burst, now).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis token bucket error: %w", err)
	}
	return result == 1, nil
}

// --- Redis 滑动窗口 ---

// RedisSlidingWindow 基于 Redis 的分布式滑动窗口限流器。
// 使用有序集合（ZSET）实现，适用于多实例部署场景。
type RedisSlidingWindow struct {
	client RedisClient
	key    string
	limit  int
	window time.Duration
}

// NewRedisSlidingWindow 创建 Redis 滑动窗口限流器。
// client 为 Redis 客户端，key 为限流键名（应全局唯一）。
// limit 为窗口内最大请求数，window 为时间窗口大小。
func NewRedisSlidingWindow(client RedisClient, key string, limit int, window time.Duration) *RedisSlidingWindow {
	return &RedisSlidingWindow{
		client: client,
		key:    key,
		limit:  limit,
		window: window,
	}
}

// slidingWindowScript 滑动窗口 Lua 脚本。
// 参数：KEYS[1]=键名, ARGV[1]=当前时间戳(毫秒), ARGV[2]=窗口大小(毫秒), ARGV[3]=限制数, ARGV[4]=唯一标识
// 返回：1=允许, 0=限流
var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

-- 移除窗口外的记录
local cutoff = now - window_ms
redis.call('zremrangebyscore', key, '-inf', cutoff)

-- 统计当前窗口内的请求数
local count = redis.call('zcard', key)

if count < limit then
    -- 添加当前请求
    redis.call('zadd', key, now, member)
    -- 设置过期时间
    redis.call('pexpire', key, window_ms + 1000)
    return 1
else
    return 0
end
`)

// Allow 检查是否允许请求通过。
func (sw *RedisSlidingWindow) Allow() bool {
	ok, _ := sw.AllowContext(context.Background())
	return ok
}

// RetryAfter 返回建议的重试等待时间：整个窗口滑过所需的最长时长。
//
// 与内存实现不同，Redis 端无法在本地得知窗口内最早记录的时间戳
// （需额外访问 Redis，而此处正处于限流路径，不应再增加负载），
// 因此给出保守上界：等满一个窗口后必定有配额可用。
// 实现 http 层的 Retry-After 语义（RFC 9110 §10.2.3）。
func (sw *RedisSlidingWindow) RetryAfter() time.Duration {
	if sw.window <= 0 {
		return 0
	}
	return sw.window
}

// AllowContext 带 context 的检查。
func (sw *RedisSlidingWindow) AllowContext(ctx context.Context) (bool, error) {
	now := time.Now().UnixMilli()
	// 成员格式：<进程唯一前缀>:<毫秒时间戳>:<进程内计数>
	// 前缀保证跨进程唯一，计数保证同进程并发唯一，两者缺一都会让 ZADD 覆盖
	// 已有成员，导致 ZCARD 低估窗口内请求数（限流被突破）。
	counter := atomic.AddUint64(&memberCounter, 1)
	member := memberPrefix + ":" + strconv.FormatInt(now, 10) + ":" + strconv.FormatUint(counter, 10)

	result, err := slidingWindowScript.Run(ctx, sw.client, []string{sw.key},
		now, sw.window.Milliseconds(), sw.limit, member).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis sliding window error: %w", err)
	}
	return result == 1, nil
}

// --- 工厂函数 ---

// StoreType 限流器存储类型。
type StoreType string

const (
	// StoreMemory 内存存储（单机）。
	StoreMemory StoreType = "memory"
	// StoreRedis Redis 存储（分布式）。
	StoreRedis StoreType = "redis"
)

// TokenBucketConfig 令牌桶配置。
type TokenBucketConfig struct {
	Rate  float64 // 每秒生成令牌数
	Burst float64 // 桶容量
}

// SlidingWindowConfig 滑动窗口配置。
type SlidingWindowConfig struct {
	Limit  int           // 窗口内最大请求数
	Window time.Duration // 时间窗口大小
}

// NewTokenBucketWithStore 根据存储类型创建令牌桶限流器。
// store 为存储类型，client 为 Redis 客户端（StoreRedis 时必须非 nil）。
// key 为限流键名（StoreRedis 时使用）。
func NewTokenBucketWithStore(store StoreType, client RedisClient, key string, cfg TokenBucketConfig) Limiter {
	switch store {
	case StoreRedis:
		return NewRedisTokenBucket(client, key, cfg.Rate, cfg.Burst)
	default:
		return NewTokenBucket(cfg.Rate, cfg.Burst)
	}
}

// NewSlidingWindowWithStore 根据存储类型创建滑动窗口限流器。
// store 为存储类型，client 为 Redis 客户端（StoreRedis 时必须非 nil）。
// key 为限流键名（StoreRedis 时使用）。
func NewSlidingWindowWithStore(store StoreType, client RedisClient, key string, cfg SlidingWindowConfig) Limiter {
	switch store {
	case StoreRedis:
		return NewRedisSlidingWindow(client, key, cfg.Limit, cfg.Window)
	default:
		return NewSlidingWindow(cfg.Limit, cfg.Window)
	}
}
