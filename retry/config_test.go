package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- 默认配置测试 ---

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig()
	assert.Equal(t, 3, c.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, c.Delay)
	assert.Equal(t, 10*time.Second, c.MaxDelay)
	assert.NotNil(t, c.RetryIf)
	assert.True(t, c.RetryIf(errors.New("test")))
}

func TestDefaultConfig_WithOptions(t *testing.T) {
	c := defaultConfig(
		WithMaxRetries(10),
		WithDelay(200*time.Millisecond),
		WithMaxDelay(30*time.Second),
		WithJitter(),
	)
	assert.Equal(t, 10, c.MaxRetries)
	assert.Equal(t, 200*time.Millisecond, c.Delay)
	assert.Equal(t, 30*time.Second, c.MaxDelay)
	assert.True(t, c.Jitter)
}
