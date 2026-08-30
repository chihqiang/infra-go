package cache

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("cache: key not found")

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
