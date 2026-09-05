package redisx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- fillDefault 测试 ---

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
	assert.Equal(t, 5*time.Minute, c.ConnMaxIdleTime)
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

func TestFillDefault_EmptyStringOverrides(t *testing.T) {
	// optional 且无 default 的 string 空值也应生效（Password/KeyPrefix 可显式置空）
	c := fillDefault(Config{Password: "", KeyPrefix: ""})
	assert.Equal(t, "", c.Password)
	assert.Equal(t, "", c.KeyPrefix)
}

func TestConfig_Sentinel(t *testing.T) {
	c := fillDefault(Config{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"sentinel1:26379", "sentinel2:26379"},
	})
	assert.Equal(t, "mymaster", c.MasterName)
	assert.Equal(t, []string{"sentinel1:26379", "sentinel2:26379"}, c.SentinelAddrs)
}
