package cache

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetAs 读取 key 并将值还原为具体类型 T，屏蔽不同后端的表示差异。
//
// 为什么需要它：Cache.Get 返回 any，而两个后端对同一个值给出不同的 Go 类型——
//   - MemCache 返回存入时的原始类型（Set("k", 5) → int(5)、Set("k", u) → user）
//   - RedisCache 必须序列化存储，读回的是 JSON 解码结果
//     （Set("k", 5) → float64(5)、Set("k", u) → map[string]any）
//
// 因此按类型断言使用 Get 的代码一旦切换后端就会 panic 或丢精度
// （int64 大整数经 float64 往返会失真）。GetAs 统一走 JSON 往返并解码到 T，
// 在两个后端上得到相同结果。
//
// 用法：
//
//	n, err := cache.GetAs[int64](ctx, c, "counter")
//	var u User
//	u, err := cache.GetAs[User](ctx, c, "user:1")
//
// 未命中时返回 (零值, ErrNotFound)。
//
// 已知限制：RedisCache 在 Get 阶段就把 JSON 数字解码为 float64，
// 因此存入的 int64 若超过 2^53，精度在 Get 时已经丢失，GetAs 无法补救。
// 需要在 Redis 后端精确保存大整数时，请以字符串形式存储（再用 GetAs 解回），
// 或改用 MemCache 后端。参见 Cache 接口注释。
func GetAs[T any](ctx context.Context, c Cache, key string) (T, error) {
	var zero T

	raw, err := c.Get(ctx, key)
	if err != nil {
		return zero, err
	}

	// 后端可能已返回 T 本身（如 MemCache 存的就是 T）
	if v, ok := raw.(T); ok {
		return v, nil
	}

	// 统一经 JSON 往返解码：Redis 后端返回 JSON 兼容表示，
	// MemCache 返回的原始值同样可序列化。
	// 直接解码到具体类型 T，因此大整数不会被中转成 float64 而丢精度。
	data, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("cache: marshal cached value for key %q: %w", key, err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, fmt.Errorf("cache: unmarshal cached value for key %q into %T: %w", key, zero, err)
	}
	return out, nil
}
