package cache

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chihqiang/infra-go/redisx"
)

// newMiniRedis 创建一个内嵌 miniredis 实例并返回 redisx 客户端与 miniredis 实例。
// 注意：miniredis 的时钟不依赖真实时间，需用 mr.FastForward() 推进以触发过期。
func newMiniRedis(t *testing.T) (*redisx.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb, err := redisx.New(redisx.Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

// newTestRedisCache 创建一个 Redis 缓存实例。
func newTestRedisCache(t *testing.T, opts ...RedisCacheOption) *RedisCache {
	t.Helper()
	rds, _ := newMiniRedis(t)
	return NewRedisCache(rds, opts...)
}

// TestRedisCacheInterface 编译期断言：RedisCache 实现 Cache 接口。
func TestRedisCacheInterface(t *testing.T) {
	rds, _ := newMiniRedis(t)
	var _ Cache = NewRedisCache(rds)
}

func TestRedisSetGet(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	assert.NoError(t, c.Set(ctx, "name", "chihqiang"))
	v, err := c.Get(ctx, "name")
	assert.NoError(t, err)
	assert.Equal(t, "chihqiang", v)
}

func TestRedisGetMiss(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	_, err := c.Get(ctx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestRedisSetStruct 去泛型后：结构体以 JSON 存入，Get 返回 map[string]any。
// 若需取回具体结构体，可先 Set 后按 map 字段断言，或使用 json.Marshal/Unmarshal 还原。
func TestRedisSetStruct(t *testing.T) {
	ctx := context.Background()
	type user struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	c := newTestRedisCache(t)

	u := user{ID: 1, Name: "chihqiang"}
	assert.NoError(t, c.Set(ctx, "user:1", u))

	got, err := c.Get(ctx, "user:1")
	assert.NoError(t, err)

	// 无目标类型信息，反序列化为 map[string]any
	m, ok := got.(map[string]any)
	require.True(t, ok, "非泛型缓存读取结构体返回 map[string]any")
	assert.Equal(t, float64(1), m["id"]) // JSON 数字默认为 float64
	assert.Equal(t, "chihqiang", m["name"])

	// 借助 JSON 还原为具体结构体
	data, err := json.Marshal(got)
	require.NoError(t, err)
	var back user
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, u, back)
}

func TestRedisSetEx(t *testing.T) {
	ctx := context.Background()
	rds, mr := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.SetEx(ctx, "k", "v", 50*time.Millisecond))
	_, err := c.Get(ctx, "k")
	assert.NoError(t, err)

	// miniredis 时钟需用 FastForward 推进（考虑 5% 抖动，多推一些）
	mr.FastForward(120 * time.Millisecond)
	_, err = c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisDel(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	assert.NoError(t, c.Set(ctx, "a", "1"))
	assert.NoError(t, c.Set(ctx, "b", "2"))
	assert.NoError(t, c.Delete(ctx, "a", "b"))

	_, err := c.Get(ctx, "a")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get(ctx, "b")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisTake(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "db-value", nil
	}

	v, err := c.Take(ctx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))

	// 第二次命中缓存，不再调用 fetch
	v, err = c.Take(ctx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestRedisTakeConcurrent(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return "db-value", nil
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			v, err := c.Take(ctx, "k", fetch)
			assert.NoError(t, err)
			assert.Equal(t, "db-value", v)
		}()
	}
	wg.Wait()

	// 并发 Take 只执行一次 fetch（防缓存击穿）
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestRedisTakeNotFound(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	// fetch 返回 ErrNotFound 表示数据不存在
	_, err := c.Take(ctx, "missing", func() (any, error) {
		return nil, ErrNotFound
	})
	assert.ErrorIs(t, err, ErrNotFound)

	// 占位符已写入：再次 Take 不应再次调用 fetch（防穿透）
	var calls int32
	_, err = c.Take(ctx, "missing", func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return nil, ErrNotFound
	})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls), "命中占位符后不应再穿透查询")
}

func TestRedisTakeFetchError(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	// 非未命名的错误原样返回
	_, err := c.Take(ctx, "k", func() (any, error) {
		return nil, assert.AnError
	})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestRedisTakeAfterExpire(t *testing.T) {
	ctx := context.Background()
	rds, mr := newMiniRedis(t)
	c := NewRedisCache(rds, WithExpire(50*time.Millisecond))

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "v", nil
	}

	v, err := c.Take(ctx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)

	mr.FastForward(120 * time.Millisecond) // 过期
	v, err = c.Take(ctx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "过期后应重新拉取")
}

func TestRedisInvalidCacheData(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds, WithCacheName("test"))

	// 直接写入非法 JSON
	assert.NoError(t, rds.Set(ctx, "bad", "not-json", 0))

	// Get 应返回未命中并删除无效缓存
	_, err := c.Get(ctx, "bad")
	assert.ErrorIs(t, err, ErrNotFound)

	// 无效缓存已被删除
	exists, err := rds.Exists(ctx, "bad")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), exists)
}

func TestRedisStoredAsJSON(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Set(ctx, "n", 42))

	raw, err := rds.Get(ctx, "n")
	assert.NoError(t, err)
	var decoded int
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	assert.Equal(t, 42, decoded)
}

func TestRedisIncrement(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Increment(ctx, "count", 5))
	assert.NoError(t, c.Increment(ctx, "count", 3))

	// 底层 INCRBY 直接存数字字符串
	raw, err := rds.Get(ctx, "count")
	assert.NoError(t, err)
	assert.Equal(t, "8", raw)
}

func TestRedisDecrement(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Decrement(ctx, "count", 2))
	assert.NoError(t, c.Decrement(ctx, "count", 3))

	raw, err := rds.Get(ctx, "count")
	assert.NoError(t, err)
	assert.Equal(t, "-5", raw)
}

func TestRedisExpire(t *testing.T) {
	ctx := context.Background()
	rds, mr := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Set(ctx, "k", "v"))
	// Redis EXPIRE 精度为秒级，用 1 秒
	assert.NoError(t, c.Expire(ctx, "k", time.Second))
	_, err := c.Get(ctx, "k")
	assert.NoError(t, err)

	mr.FastForward(2 * time.Second) // 越过过期时间
	_, err = c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)

	// key 不存在返回 ErrNotFound
	err = c.Expire(ctx, "missing", time.Minute)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisExpireImmediate(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Set(ctx, "k", "v"))
	// ttl <= 0 立即失效
	assert.NoError(t, c.Expire(ctx, "k", 0))
	_, err := c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}
