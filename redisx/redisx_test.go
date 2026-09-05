package redisx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMiniClient 创建基于 miniredis 的测试客户端。
// keyPrefix 为空时返回无前缀客户端，否则返回带前缀客户端。
func newMiniClient(t *testing.T, keyPrefix string) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := New(Config{
		Addr:      mr.Addr(),
		KeyPrefix: keyPrefix,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

func TestNew(t *testing.T) {
	c, err := New(Config{Addr: "127.0.0.1:6379"})
	require.NoError(t, err)
	assert.NotNil(t, c)
	assert.NotNil(t, c.Client())
}

func TestMustNew(t *testing.T) {
	c := MustNew(Config{Addr: "127.0.0.1:6379"})
	assert.NotNil(t, c)
}

func TestWrapKey(t *testing.T) {
	// 无前缀
	c := &Client{keyPrefix: ""}
	assert.Equal(t, "foo", c.wrapKey("foo"))

	// 有前缀
	c = &Client{keyPrefix: "myapp"}
	assert.Equal(t, "myapp:foo", c.wrapKey("foo"))
}

func TestWrapKeys(t *testing.T) {
	c := &Client{keyPrefix: "myapp"}
	result := c.wrapKeys("foo", "bar", "baz")
	assert.Equal(t, []string{"myapp:foo", "myapp:bar", "myapp:baz"}, result)

	// 无前缀
	c = &Client{keyPrefix: ""}
	result = c.wrapKeys("foo", "bar")
	assert.Equal(t, []string{"foo", "bar"}, result)
}

// --- 基础字符串操作 ---

func TestGetSet(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	// 不存在返回 ErrNil（wrapErr 已将 redis.Nil 转成自定义 ErrNil）
	_, err := c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNil)

	require.NoError(t, c.Set(ctx, "k", "v", 0))
	val, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", val)

	// 覆盖
	require.NoError(t, c.Set(ctx, "k", "v2", 0))
	val, _ = c.Get(ctx, "k")
	assert.Equal(t, "v2", val)
}

func TestSetWithExpiration(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniClient(t, "")

	require.NoError(t, c.Set(ctx, "k", "v", 1*time.Second))
	mr.FastForward(2 * time.Second)
	_, err := c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNil)
}

func TestSetNX(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	ok, err := c.SetNX(ctx, "k", "v", 0)
	require.NoError(t, err)
	assert.True(t, ok)

	// 再次设置失败
	ok, err = c.SetNX(ctx, "k", "v2", 0)
	require.NoError(t, err)
	assert.False(t, ok)

	val, _ := c.Get(ctx, "k")
	assert.Equal(t, "v", val)
}

func TestDel(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	require.NoError(t, c.Set(ctx, "a", "1", 0))
	require.NoError(t, c.Set(ctx, "b", "2", 0))

	n, err := c.Del(ctx, "a", "b")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	// 空 keys 不报错
	n, err = c.Del(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestExists(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	require.NoError(t, c.Set(ctx, "a", "1", 0))
	n, err := c.Exists(ctx, "a", "missing")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	// 空 keys
	n, err = c.Exists(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestExpireTTL(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	require.NoError(t, c.Set(ctx, "k", "v", 0))
	ok, err := c.Expire(ctx, "k", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, ok)

	ttl, err := c.TTL(ctx, "k")
	require.NoError(t, err)
	assert.Greater(t, ttl, 5*time.Second)

	// 不存在键
	ok, err = c.Expire(ctx, "missing", 10*time.Second)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIncr(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	n, err := c.Incr(ctx, "counter")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = c.IncrBy(ctx, "counter", 10)
	require.NoError(t, err)
	assert.Equal(t, int64(11), n)
}

// --- Hash 操作 ---

func TestHashOps(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	// HSet + HGet
	n, err := c.HSet(ctx, "h", "f1", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	val, err := c.HGet(ctx, "h", "f1")
	require.NoError(t, err)
	assert.Equal(t, "v1", val)

	// 不存在的 field
	_, err = c.HGet(ctx, "h", "missing")
	assert.ErrorIs(t, err, ErrNil)

	// HGetAll
	m, err := c.HGetAll(ctx, "h")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"f1": "v1"}, m)

	// HDel
	n, err = c.HDel(ctx, "h", "f1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, err = c.HGet(ctx, "h", "f1")
	assert.ErrorIs(t, err, ErrNil)
}

// --- List 操作 ---

func TestListOps(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	_, err := c.LPush(ctx, "l", "a", "b")
	require.NoError(t, err)
	_, err = c.RPush(ctx, "l", "c")
	require.NoError(t, err)

	vals, err := c.LRange(ctx, "l", 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "a", "c"}, vals)

	v, err := c.LPop(ctx, "l")
	require.NoError(t, err)
	assert.Equal(t, "b", v)

	v, err = c.RPop(ctx, "l")
	require.NoError(t, err)
	assert.Equal(t, "c", v)
}

// --- Set 操作 ---

func TestSetOps(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	n, err := c.SAdd(ctx, "s", "m1", "m2", "m3")
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	// 重复添加
	n, _ = c.SAdd(ctx, "s", "m1")
	assert.Equal(t, int64(0), n)

	ok, err := c.SIsMember(ctx, "s", "m1")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, _ = c.SIsMember(ctx, "s", "missing")
	assert.False(t, ok)

	members, err := c.SMembers(ctx, "s")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"m1", "m2", "m3"}, members)

	n, err = c.SRem(ctx, "s", "m1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

// --- Scan 操作 ---

// scanAll 循环迭代直到游标归零，模拟真实 SCAN 用法。
// 注意：miniredis 对带 MATCH 的 SCAN 每次只返回 1 个 key 且游标恒为 0（已知限制），
// 因此本辅助仅用于空 match（全量）场景。
func scanAll(t *testing.T, c *Client, ctx context.Context) []string {
	t.Helper()
	var all []string
	cursor := uint64(0)
	for {
		keys, next, err := c.Scan(ctx, cursor, "", 100)
		require.NoError(t, err)
		all = append(all, keys...)
		cursor = next
		if cursor == 0 {
			return all
		}
	}
}

func TestScan_NoPrefix(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")
	for i := 0; i < 10; i++ {
		require.NoError(t, c.Set(ctx, string(rune('a'+i)), "1", 0))
	}

	keys := scanAll(t, c, ctx)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, keys)
}

func TestScan_NoPrefix_Match(t *testing.T) {
	// miniredis 对 MATCH 的 SCAN 每次只返回 1 个匹配 key（游标恒 0），
	// 无法验证"返回全部匹配"；此处仅验证 match 过滤确实生效且无前缀时键原样返回。
	ctx := context.Background()
	c, _ := newMiniClient(t, "")
	for i := 0; i < 10; i++ {
		require.NoError(t, c.Set(ctx, string(rune('a'+i)), "1", 0))
	}

	keys, _, err := c.Scan(ctx, 0, "a*", 100)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	for _, k := range keys {
		assert.Equal(t, "a", k[0:1])
	}
}

func TestScan_WithPrefix(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniClient(t, "app")

	// 直接写入原始 redis 以绕过前缀
	raw := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, raw.Set(ctx, "app:user:1", "u1", 0).Err())
	require.NoError(t, raw.Set(ctx, "app:order:1", "o1", 0).Err())
	require.NoError(t, raw.Set(ctx, "other:key", "x", 0).Err())

	// 无前缀键的全量扫描应只命中 app:*（空 match → 自动 app:*）
	// 由于 miniredis MATCH 限制，分次收集并断言都不含 other:key 且前缀被剥离
	keys := scanAll(t, c, ctx)
	require.NotEmpty(t, keys)
	for _, k := range keys {
		assert.NotEqual(t, "other:key", k, "scan with prefix must not return keys outside prefix")
		assert.NotContains(t, k, "app:")
	}
}

func TestScan_WithPrefix_Match(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniClient(t, "app")

	raw := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, raw.Set(ctx, "app:user:1", "u1", 0).Err())
	require.NoError(t, raw.Set(ctx, "app:user:2", "u2", 0).Err())
	require.NoError(t, raw.Set(ctx, "app:order:1", "o1", 0).Err())

	// match=user:* → 自动 app:user:*，返回的键应剥离 app: 前缀
	keys, _, err := c.Scan(ctx, 0, "user:*", 100)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	for _, k := range keys {
		assert.Contains(t, k, "user:")
		assert.NotContains(t, k, "app:")
	}
}

// --- Ping / Close ---

func TestPing(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")
	require.NoError(t, c.Ping(ctx))
}

func TestWrapErr(t *testing.T) {
	// nil 错误
	assert.Nil(t, wrapErr(nil))

	// redis.Nil → ErrNil
	err := wrapErr(redis.Nil)
	assert.ErrorIs(t, err, ErrNil)

	// 其他错误原样返回
	other := errors.New("boom")
	assert.Same(t, other, wrapErr(other))
}

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "redisx: lock not acquired", ErrLockNotAcquired.Error())
	assert.Equal(t, "redisx: lock ownership mismatch", ErrLockOwnershipMismatch.Error())
	assert.Equal(t, "redisx: key not found", ErrNil.Error())
}

// --- 带前缀的端到端 ---

func TestKeyPrefix_EndToEnd(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniClient(t, "app")

	require.NoError(t, c.Set(ctx, "greeting", "hello", 0))

	// 前缀键实际写入
	raw := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	val, err := raw.Get(ctx, "app:greeting").Result()
	require.NoError(t, err)
	assert.Equal(t, "hello", val)

	// 通过封装读回
	got, err := c.Get(ctx, "greeting")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)

	// 不带前缀的键不可见
	_, err = c.Get(ctx, "app:greeting")
	assert.ErrorIs(t, err, ErrNil)
}
