package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件验证 GetAs 在两个后端的返回值一致（消除 Cache.Get 的类型差异）。

type getAsUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// bothBackends 返回两个后端的实例，便于对同一组断言跑两遍。
func bothBackends(t *testing.T) map[string]Cache {
	t.Helper()
	ctx := context.Background()

	mem := NewMemCache(ctx, time.Minute)
	t.Cleanup(mem.Close)

	rds, _ := newMiniRedis(t)

	return map[string]Cache{
		"mem":   mem,
		"redis": NewRedisCache(rds),
	}
}

// TestGetAs_ScalarConsistentAcrossBackends 验证标量在两个后端读回一致的类型。
// 直接用 Get 时 mem 返回 int、redis 返回 float64，按 int 断言会在 redis 上失败。
func TestGetAs_ScalarConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "count", 42))

			n, err := GetAs[int](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, 42, n)

			n64, err := GetAs[int64](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, int64(42), n64)

			f, err := GetAs[float64](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, float64(42), f)

			// 字符串值在两个后端都应原样读回
			require.NoError(t, c.Set(ctx, "name", "chihqiang"))
			s, err := GetAs[string](ctx, c, "name")
			require.NoError(t, err)
			assert.Equal(t, "chihqiang", s)
		})
	}
}

// TestGetAs_Int64WithinFloat64Range 验证在 float64 可精确表示的范围内，
// 两个后端都能原样读回 int64。
func TestGetAs_Int64WithinFloat64Range(t *testing.T) {
	ctx := context.Background()
	const exact = int64(9007199254740992) // 2^53，float64 可精确表示

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "big", exact))

			got, err := GetAs[int64](ctx, c, "big")
			require.NoError(t, err)
			assert.Equal(t, exact, got)
		})
	}
}

// TestGetAs_LargeInt64PrecisionMem 验证 MemCache 能精确保存超过 2^53 的 int64。
//
// Redis 后端的对应限制见 GetAs 的文档：RedisCache.Get 会把 JSON 数字解码为
// float64，精度在 Get 阶段即已丢失，GetAs 无法补救。因此这里只对 mem 断言；
// 若将来把 RedisCache 改为 UseNumber 解码，应把本用例扩展到两个后端。
func TestGetAs_LargeInt64PrecisionMem(t *testing.T) {
	ctx := context.Background()
	const big = int64(9007199254740993) // 2^53 + 1

	mem := NewMemCache(ctx, time.Minute)
	defer mem.Close()

	require.NoError(t, mem.Set(ctx, "big", big))

	got, err := GetAs[int64](ctx, mem, "big")
	require.NoError(t, err)
	assert.Equal(t, big, got, "MemCache must preserve large int64 exactly")
}

// TestGetAs_StructConsistentAcrossBackends 验证结构体在两个后端都能还原为具体类型。
// 直接用 Get 时 mem 返回原结构体、redis 返回 map[string]any。
func TestGetAs_StructConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()
	want := getAsUser{ID: 7, Name: "chihqiang"}

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "user:1", want))

			got, err := GetAs[getAsUser](ctx, c, "user:1")
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestGetAs_SliceConsistentAcrossBackends 验证切片类型。
func TestGetAs_SliceConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "tags", []string{"a", "b"}))

			got, err := GetAs[[]string](ctx, c, "tags")
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, got)
		})
	}
}

// TestGetAs_Miss 验证未命中时返回零值与 ErrNotFound。
func TestGetAs_Miss(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			n, err := GetAs[int](ctx, c, "missing")
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Equal(t, 0, n)

			s, err := GetAs[string](ctx, c, "missing")
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Equal(t, "", s)
		})
	}
}

// TestGetAs_TypeMismatch 验证无法解码为 T 时返回错误而不是静默零值。
func TestGetAs_TypeMismatch(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "user", map[string]any{"id": 1}))

			// 对象 → 标量类型不兼容，应报错而不是返回 0
			_, err := GetAs[int](ctx, c, "user")
			assert.Error(t, err)
		})
	}
}

// TestGetAs_TakeWrittenValues 验证 Take 写入的值也能用 GetAs 读回。
func TestGetAs_TakeWrittenValues(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			fetch := func() (any, error) { return getAsUser{ID: 9, Name: "from-fetch"}, nil }
			_, err := c.Take(ctx, "u", fetch)
			require.NoError(t, err)

			got, err := GetAs[getAsUser](ctx, c, "u")
			require.NoError(t, err)
			assert.Equal(t, getAsUser{ID: 9, Name: "from-fetch"}, got)
		})
	}
}
