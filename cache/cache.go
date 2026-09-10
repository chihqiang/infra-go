package cache

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("cache: key not found")

// Cache 统一缓存接口，内存与 Redis 两种实现共享同一组方法。
//
// 注意 Get/Take 的返回值类型随后端而不同：
//   - MemCache 返回存入时的原始类型（Set(ctx,"k",5) 读回 int(5)；
//     Set(ctx,"k",u) 读回 u 本身）
//   - RedisCache 必须序列化存储，读回的是 JSON 解码结果
//     （Set(ctx,"k",5) 读回 float64(5)；Set(ctx,"k",u) 读回 map[string]any）
//
// 因此直接对 Get 的返回值做类型断言（如 v.(int)）的代码在切换后端后会 panic，
// 且 int64 大整数经 Redis 的 float64 往返会丢精度。
// 需要后端无关的读取时请使用 GetAs[T]，它统一经 JSON 往返解码到具体类型：
//
//	n, err := cache.GetAs[int64](ctx, c, "counter")
type Cache interface {
	Get(ctx context.Context, key string) (any, error)
	Set(ctx context.Context, key string, value any) error
	SetEx(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Take(ctx context.Context, key string, fetch func() (any, error)) (any, error)
	// Increment 将 key 对应的数值自增 delta；key 不存在时初始化为 delta。
	Increment(ctx context.Context, key string, delta int64) error
	// Decrement 将 key 对应的数值自减 delta；key 不存在时初始化为 -delta。
	Decrement(ctx context.Context, key string, delta int64) error
	// Expire 为 key 设置存活时间 ttl，到期后自动失效；ttl <= 0 时立即失效。
	Expire(ctx context.Context, key string, ttl time.Duration) error
}
