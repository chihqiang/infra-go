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

// newMiniClient creates a test client backed by miniredis.
// An empty keyPrefix returns a client without a prefix, otherwise a client carrying the
// given prefix is returned.
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
	// No prefix
	c := &Client{keyPrefix: ""}
	assert.Equal(t, "foo", c.wrapKey("foo"))

	// With prefix
	c = &Client{keyPrefix: "myapp"}
	assert.Equal(t, "myapp:foo", c.wrapKey("foo"))
}

func TestWrapKeys(t *testing.T) {
	c := &Client{keyPrefix: "myapp"}
	result := c.wrapKeys("foo", "bar", "baz")
	assert.Equal(t, []string{"myapp:foo", "myapp:bar", "myapp:baz"}, result)

	// No prefix
	c = &Client{keyPrefix: ""}
	result = c.wrapKeys("foo", "bar")
	assert.Equal(t, []string{"foo", "bar"}, result)
}

// --- Basic string operations ---

func TestGetSet(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	// A missing key returns ErrNil (wrapErr already converts redis.Nil into the custom
	// ErrNil)
	_, err := c.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNil)

	require.NoError(t, c.Set(ctx, "k", "v", 0))
	val, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", val)

	// Overwrite
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

	// A second SetNX fails
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

	// Empty keys must not error
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

	// Empty keys
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

	// A missing key
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

// --- Hash operations ---

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

	// A missing field
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

// --- List operations ---

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

// --- Set operations ---

func TestSetOps(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniClient(t, "")

	n, err := c.SAdd(ctx, "s", "m1", "m2", "m3")
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	// Adding a duplicate
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

// --- Scan operations ---

// scanAll iterates until the cursor returns to zero, mimicking real SCAN usage.
// Note: for SCAN with MATCH, miniredis returns only 1 key per call and always keeps the
// cursor at 0 (a known limitation), so this helper is only used with an empty match
// (full scan).
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
	// For SCAN with MATCH, miniredis returns only 1 matching key per call (the cursor
	// stays 0), so "returns all matches" cannot be verified; this only verifies that the
	// match filter really works and that keys come back unchanged when there is no prefix.
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

	// Write to raw redis directly to bypass the prefix
	raw := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, raw.Set(ctx, "app:user:1", "u1", 0).Err())
	require.NoError(t, raw.Set(ctx, "app:order:1", "o1", 0).Err())
	require.NoError(t, raw.Set(ctx, "other:key", "x", 0).Err())

	// A full scan without a prefix key should only hit app:* (empty match -> app:*
	// automatically). Because of the miniredis MATCH limitation, keys are collected
	// across calls and asserted to exclude other:key with the prefix stripped.
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

	// match=user:* -> app:user:* automatically, and the returned keys have the app: prefix
	// stripped
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
	// nil error
	assert.Nil(t, wrapErr(nil))

	// redis.Nil -> ErrNil
	err := wrapErr(redis.Nil)
	assert.ErrorIs(t, err, ErrNil)

	// Other errors are returned as they are
	other := errors.New("boom")
	assert.Same(t, other, wrapErr(other))
}

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "redisx: lock not acquired", ErrLockNotAcquired.Error())
	assert.Equal(t, "redisx: lock ownership mismatch", ErrLockOwnershipMismatch.Error())
	assert.Equal(t, "redisx: key not found", ErrNil.Error())
}

// --- End to end with a key prefix ---

func TestKeyPrefix_EndToEnd(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniClient(t, "app")

	require.NoError(t, c.Set(ctx, "greeting", "hello", 0))

	// The prefixed key is actually written
	raw := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	val, err := raw.Get(ctx, "app:greeting").Result()
	require.NoError(t, err)
	assert.Equal(t, "hello", val)

	// Read it back through the wrapper
	got, err := c.Get(ctx, "greeting")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)

	// The key without the prefix is invisible
	_, err = c.Get(ctx, "app:greeting")
	assert.ErrorIs(t, err, ErrNil)
}
