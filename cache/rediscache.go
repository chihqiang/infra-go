package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/redisx"
	"github.com/chihqiang/infra-go/syncx"
)

// 默认配置。
const (
	// 默认过期时间：7 天，防止 Redis 中缓存无限堆积。
	defaultRedisExpiry = time.Hour * 24 * 7
	// 默认未命中占位符过期时间：1 分钟，避免热点不存在 key 反复穿透 DB。
	defaultRedisNotFoundExpiry = time.Minute
	// 默认缓存名称，用于日志标识。
	defaultRedisCacheName = "redis_cache"
)

// notFoundPlaceholder 未命中占位符，用于防缓存穿透。
const notFoundPlaceholder = "*"

// errPlaceholder 内部错误：命中未命中占位符。
// 与 ErrNotFound 的区别在于：占位符表示"该 key 确认不存在"，
// Take 命中占位符时不会再次穿透查询 DB。
var errPlaceholder = errors.New("cache: placeholder")

// RedisCache 是 Cache 接口的 Redis 实现。
//
// 特性：
//   - 值以 JSON 序列化存储
//   - 防缓存击穿：Take 通过 SingleFlight 合并相同 key 的并发请求
//   - 防缓存穿透：查询无结果时写入短时占位符
//   - 防缓存雪崩：过期时间带轻微抖动
//   - 快速失败：Redis 故障时不穿透到 DB
//
// 内存实现见 MemCache。两者均实现 Cache 接口，可在本地/分布式间切换。
type RedisCache struct {
	rds            *redisx.Client
	expiry         time.Duration
	notFoundExpiry time.Duration
	barrier        *syncx.SingleFlight[any]
	unstable       *unstable
	name           string
}

// RedisCacheOption 用于自定义 Redis 缓存行为。
type RedisCacheOption func(*redisCacheOptions)

// redisCacheOptions Redis 缓存内部选项。
type redisCacheOptions struct {
	expiry         time.Duration // 默认过期时间
	notFoundExpiry time.Duration // 未命中占位符过期时间
	name           string        // 缓存名称，用于日志标识
}

// WithExpire 设置默认过期时间。
// 未设置时默认 7 天。
func WithExpire(d time.Duration) RedisCacheOption {
	return func(o *redisCacheOptions) { o.expiry = d }
}

// WithNotFoundExpire 设置未命中占位符的过期时间。
// 占位符用于防缓存穿透：查询无结果时短暂缓存"不存在"标记。
// 未设置时默认 1 分钟。
func WithNotFoundExpire(d time.Duration) RedisCacheOption {
	return func(o *redisCacheOptions) { o.notFoundExpiry = d }
}

// WithCacheName 设置缓存名称，用于日志标识。
func WithCacheName(name string) RedisCacheOption {
	return func(o *redisCacheOptions) { o.name = name }
}

// NewRedisCache 创建并返回一个基于 Redis 的缓存实例。
// rds 为 redisx 客户端；通过选项可定制默认过期时间、占位符过期时间、名称等。
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

// Get 返回指定 key 的值；未命中或命中占位符返回 ErrNotFound。
func (c *RedisCache) Get(ctx context.Context, key string) (any, error) {
	v, err := c.doGet(ctx, key)
	if errors.Is(err, errPlaceholder) {
		return nil, ErrNotFound
	}
	return v, err
}

// Set 将 value 写入缓存，使用默认过期时间。
func (c *RedisCache) Set(ctx context.Context, key string, value any) error {
	return c.SetEx(ctx, key, value, c.expiry)
}

// SetEx 将 value 写入缓存并指定存活时间 ttl。
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

// Delete 删除一个或多个 key。
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := c.rds.Del(ctx, keys...)
	return err
}

// Take 返回 key 的值；未命中时调用 fetch 获取并写入缓存。
// 防击穿：相同 key 并发只执行一次 fetch；
// 防穿透：fetch 返回 ErrNotFound 时写入短时占位符；
// 快速失败：Redis 故障时不穿透到 DB。
func (c *RedisCache) Take(ctx context.Context, key string, fetch func() (any, error)) (any, error) {
	val, err := c.barrier.Do(key, func() (any, error) {
		// 二次检查：等待期间可能已被其它并发请求写入
		v, e := c.doGet(ctx, key)
		if e == nil {
			return v, nil
		}
		if errors.Is(e, errPlaceholder) {
			// 命中占位符：该 key 确认不存在，直接返回未命中，不穿透 DB
			return nil, ErrNotFound
		}
		if !errors.Is(e, ErrNotFound) {
			// Redis 故障：快速失败，不把请求穿透到 DB
			return nil, e
		}

		v, e = fetch()
		if e != nil {
			if errors.Is(e, ErrNotFound) {
				// DB 也无数据：写入短时占位符，防缓存穿透
				if err := c.setNotFound(ctx, key); err != nil {
					logger.InfofCtx(ctx, "cache(%s): set not found placeholder failed, key: %s, error: %v",
						c.name, key, err)
				}
				return nil, ErrNotFound
			}
			return nil, e
		}

		// 缓存写失败不影响主流程（与防击穿语义一致），仅记录日志
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

// Increment 将 key 对应的数值自增 delta；key 不存在时初始化为 delta。
// 底层使用 Redis INCRBY，原子操作。
func (c *RedisCache) Increment(ctx context.Context, key string, delta int64) error {
	_, err := c.rds.IncrBy(ctx, key, delta)
	return err
}

// Decrement 将 key 对应的数值自减 delta；key 不存在时初始化为 -delta。
// 底层使用 Redis INCRBY 传负值，原子操作。
func (c *RedisCache) Decrement(ctx context.Context, key string, delta int64) error {
	_, err := c.rds.IncrBy(ctx, key, -delta)
	return err
}

// Expire 为 key 设置存活时间 ttl，到期后自动失效。
// ttl <= 0 时立即失效（删除该 key）；key 不存在返回 ErrNotFound。
// 注意：Redis EXPIRE 精度为秒级，此处直接透传 ttl，不加抖动（避免被截断为 0 秒立即删除）。
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

// doGet 内部读取：区分未命中（ErrNotFound）与占位符（errPlaceholder）。
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

	// 去泛型后无法在编译期知道目标类型，统一反序列化为 map[string]any。
	// 读取结构体/指针等复杂类型时需由调用方自行类型断言或转换（见 cache.md）。
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		// 反序列化失败：删除无效缓存并返回未命中，让上层重新加载
		logger.InfofCtx(ctx, "cache(%s): unmarshal cache failed, key: %s, error: %v", c.name, key, err)
		_, _ = c.rds.Del(ctx, key)
		return nil, ErrNotFound
	}
	return v, nil
}

// setNotFound 写入未命中占位符，带抖动过期时间。
func (c *RedisCache) setNotFound(ctx context.Context, key string) error {
	_, err := c.rds.SetNX(ctx, key, notFoundPlaceholder, c.aroundDuration(c.notFoundExpiry))
	return err
}

// aroundDuration 返回带抖动的过期时间，避免大量 key 同时过期（雪崩防护）。
func (c *RedisCache) aroundDuration(d time.Duration) time.Duration {
	return c.unstable.AroundDuration(d)
}
