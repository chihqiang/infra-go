package redisx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, "127.0.0.1:6379", c.Addr)
	assert.Equal(t, "", c.Username)
	assert.Equal(t, "", c.Password)
	assert.Equal(t, 0, c.DB)
	assert.Equal(t, 10, c.PoolSize)
	assert.Equal(t, 2, c.MinIdleConns)
	assert.Equal(t, 3, c.MaxRetries)
	assert.Equal(t, 5*time.Second, c.DialTimeout)
	assert.Equal(t, 3*time.Second, c.ReadTimeout)
	assert.Equal(t, 3*time.Second, c.WriteTimeout)
	assert.Equal(t, 4*time.Second, c.PoolTimeout)
	assert.Equal(t, 5*time.Minute, c.IdleConnTimeout)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := fillDefault(Config{
		Addr:         "redis.example.com:6380",
		Password:     "secret",
		DB:           2,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   5,
		DialTimeout:  10 * time.Second,
		KeyPrefix:    "myapp",
	})

	assert.Equal(t, "redis.example.com:6380", c.Addr)
	assert.Equal(t, "secret", c.Password)
	assert.Equal(t, 2, c.DB)
	assert.Equal(t, 20, c.PoolSize)
	assert.Equal(t, 5, c.MinIdleConns)
	assert.Equal(t, 5, c.MaxRetries)
	assert.Equal(t, 10*time.Second, c.DialTimeout)
	assert.Equal(t, "myapp", c.KeyPrefix)
}

func TestNew(t *testing.T) {
	c, err := New(Config{
		Addr: "127.0.0.1:6379",
	})
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

func TestGenerateToken(t *testing.T) {
	token1, err := generateToken()
	require.NoError(t, err)
	assert.Len(t, token1, 32) // 16 bytes -> 32 hex chars

	token2, err := generateToken()
	require.NoError(t, err)
	assert.NotEqual(t, token1, token2)
}

func TestIsLockNotAcquired(t *testing.T) {
	assert.True(t, IsLockNotAcquired(ErrLockNotAcquired))
	assert.False(t, IsLockNotAcquired(nil))
	assert.False(t, IsLockNotAcquired(ErrLockOwnershipMismatch))
}

func TestLocker(t *testing.T) {
	c := MustNew(Config{Addr: "127.0.0.1:6379"})
	la := c.Locker("test-lock", 10*time.Second)
	assert.NotNil(t, la)
}

func TestWrapErr(t *testing.T) {
	// nil 错误
	assert.Nil(t, wrapErr(nil))

	// redis.Nil 错误需要构造
	// 直接测试 ErrNil
	assert.Equal(t, "redisx: key not found", ErrNil.Error())
}

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "redisx: lock not acquired", ErrLockNotAcquired.Error())
	assert.Equal(t, "redisx: lock ownership mismatch", ErrLockOwnershipMismatch.Error())
	assert.Equal(t, "redisx: key not found", ErrNil.Error())
}

func TestConfig_Sentinel(t *testing.T) {
	c := fillDefault(Config{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"sentinel1:26379", "sentinel2:26379"},
	})
	assert.Equal(t, "mymaster", c.MasterName)
	assert.Equal(t, []string{"sentinel1:26379", "sentinel2:26379"}, c.SentinelAddrs)
}
