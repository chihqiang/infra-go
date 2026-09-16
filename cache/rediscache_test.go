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

// newMiniRedis creates an embedded miniredis instance and returns the redisx client
// together with the miniredis instance.
// Note: the miniredis clock does not follow real time, so mr.FastForward() must be
// used to advance it and trigger expiries.
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

// newTestRedisCache creates a Redis cache instance.
func newTestRedisCache(t *testing.T, opts ...RedisCacheOption) *RedisCache {
	t.Helper()
	rds, _ := newMiniRedis(t)
	return NewRedisCache(rds, opts...)
}

// TestRedisCacheInterface is a compile-time assertion: RedisCache implements the Cache
// interface.
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

// TestRedisSetStruct, without generics: a struct is stored as JSON and Get returns a
// map[string]any.
// To get the concrete struct back, either assert on the map fields or restore it with
// json.Marshal/Unmarshal.
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

	// No target type information, so it is deserialized into map[string]any
	m, ok := got.(map[string]any)
	require.True(t, ok, "reading a struct from a non-generic cache returns map[string]any")
	assert.Equal(t, json.Number("1"), m["id"]) // UseNumber: JSON numbers decode to json.Number
	assert.Equal(t, "chihqiang", m["name"])

	// Restore the concrete struct through JSON
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

	// The miniredis clock must be advanced with FastForward (allowing for the 5%
	// jitter, push a bit further)
	mr.FastForward(120 * time.Millisecond)
	_, err = c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestRedisGetLargeInt64Precision verifies that Get does not lose precision for an
// int64 above 2^53.
// doGet decodes with json.Decoder.UseNumber(), which keeps numbers as json.Number
// rather than float64; otherwise large integers such as snowflake IDs or nanosecond
// timestamps would be distorted in a float64 round-trip.
func TestRedisGetLargeInt64Precision(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	const big = int64(9007199254740993) // 2^53 + 1, not exactly representable as a float64
	require.NoError(t, c.Set(ctx, "big", big))

	got, err := c.Get(ctx, "big")
	require.NoError(t, err)
	num, ok := got.(json.Number)
	require.True(t, ok, "a JSON number must decode to json.Number, got %T", got)
	assert.Equal(t, "9007199254740993", num.String())

	n, err := num.Int64()
	require.NoError(t, err)
	assert.Equal(t, big, n)
}

// TestRedisGetTrailingGarbage verifies that trailing garbage is still treated as dirty
// data (consistent with the strictness of json.Unmarshal).
func TestRedisGetTrailingGarbage(t *testing.T) {
	ctx := context.Background()
	rds, mr := newMiniRedis(t)
	c := NewRedisCache(rds)

	// Bypass Set and write dirty data directly to simulate a write by an external
	// component
	mr.Set("dirty", `{"a":1}garbage`)

	_, err := c.Get(ctx, "dirty")
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

	// The second call hits the cache and no longer calls fetch
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

	// Concurrent Take calls run fetch only once (stampede protection)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestRedisTakeNotFound(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	// fetch returning ErrNotFound means the data does not exist
	_, err := c.Take(ctx, "missing", func() (any, error) {
		return nil, ErrNotFound
	})
	assert.ErrorIs(t, err, ErrNotFound)

	// The placeholder has been written: a second Take must not call fetch again
	// (penetration protection)
	var calls int32
	_, err = c.Take(ctx, "missing", func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return nil, ErrNotFound
	})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls), "a placeholder hit must not penetrate again")
}

func TestRedisTakeFetchError(t *testing.T) {
	ctx := context.Background()
	c := newTestRedisCache(t)

	// Errors other than not-found are returned as they are
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

	mr.FastForward(120 * time.Millisecond) // expired
	v, err = c.Take(ctx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "must fetch again after expiry")
}

func TestRedisInvalidCacheData(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds, WithCacheName("test"))

	// Write invalid JSON directly (simulating another component writing the same key in
	// a non-JSON format)
	assert.NoError(t, rds.Set(ctx, "bad", "not-json", 0))

	// Get must return a miss so the caller reloads
	_, err := c.Get(ctx, "bad")
	assert.ErrorIs(t, err, ErrNotFound)

	// But the key must NOT be deleted: it may belong to another component that uses the
	// same key in a non-JSON format, and deleting it automatically would cause the subtle
	// failure of "the other side just wrote it and this cache deleted it".
	// See TestRedisInvalidCacheData_MustNotDeleteKey for details.
	exists, err := rds.Exists(ctx, "bad")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), exists, "invalid data must be left alone, not deleted")
}

// TestRedisInvalidCacheData_MustNotDeleteKey is a regression test: a failed
// deserialization must not delete the key.
//
// Historical defect: doGet called Del(key) when json.Unmarshal failed, so components
// sharing a key deleted each other's data - showing up as the subtle "written and
// immediately deleted" failure.
func TestRedisInvalidCacheData_MustNotDeleteKey(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds, WithCacheName("test"))

	// Simulate another component writing a raw string directly with redisx
	const raw = "some-plain-value"
	require.NoError(t, rds.Set(ctx, "shared", raw, 0))

	// This cache fails to read it and returns a miss
	_, err := c.Get(ctx, "shared")
	require.ErrorIs(t, err, ErrNotFound)

	// The original data must be preserved as it is
	got, err := rds.Get(ctx, "shared")
	require.NoError(t, err)
	assert.Equal(t, raw, got, "another component's data must not be deleted")
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

	// The underlying INCRBY stores a numeric string directly
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
	// Redis EXPIRE has second-level precision, so use 1 second
	assert.NoError(t, c.Expire(ctx, "k", time.Second))
	_, err := c.Get(ctx, "k")
	assert.NoError(t, err)

	mr.FastForward(2 * time.Second) // past the expiry
	_, err = c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)

	// a missing key returns ErrNotFound
	err = c.Expire(ctx, "missing", time.Minute)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisExpireImmediate(t *testing.T) {
	ctx := context.Background()
	rds, _ := newMiniRedis(t)
	c := NewRedisCache(rds)

	assert.NoError(t, c.Set(ctx, "k", "v"))
	// ttl <= 0 expires immediately
	assert.NoError(t, c.Expire(ctx, "k", 0))
	_, err := c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}
