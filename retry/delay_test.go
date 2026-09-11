package retry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- 延迟策略测试 ---

func TestExponentialBackoff_Func(t *testing.T) {
	fn := ExponentialBackoff(10*time.Millisecond, 2)
	assert.Equal(t, 10*time.Millisecond, fn(1, 0))
	assert.Equal(t, 20*time.Millisecond, fn(2, 0))
	assert.Equal(t, 40*time.Millisecond, fn(3, 0))
}

func TestFixedDelay_Func(t *testing.T) {
	fn := FixedDelay(100 * time.Millisecond)
	assert.Equal(t, 100*time.Millisecond, fn(1, 0))
	assert.Equal(t, 100*time.Millisecond, fn(2, 0))
	assert.Equal(t, 100*time.Millisecond, fn(3, 0))
}

func TestLinearDelay_Func(t *testing.T) {
	fn := LinearDelay(10*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, 10*time.Millisecond, fn(1, 0))
	assert.Equal(t, 15*time.Millisecond, fn(2, 0))
	assert.Equal(t, 20*time.Millisecond, fn(3, 0))
}

// --- computeDelay 测试 ---

func TestComputeDelay_Default(t *testing.T) {
	c := defaultConfig()
	// attempt 1: 100ms * 2^0 = 100ms
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 100*time.Millisecond, d)
	// attempt 2: 100ms * 2^1 = 200ms
	d = computeDelay(c, 2, 0)
	assert.Equal(t, 200*time.Millisecond, d)
	// attempt 3: 100ms * 2^2 = 400ms
	d = computeDelay(c, 3, 0)
	assert.Equal(t, 400*time.Millisecond, d)
}

func TestComputeDelay_CustomFunc(t *testing.T) {
	c := defaultConfig(WithDelayFunc(FixedDelay(50 * time.Millisecond)))
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 50*time.Millisecond, d)
}

func TestComputeDelay_MaxDelayCap(t *testing.T) {
	c := defaultConfig(WithDelay(1*time.Hour), WithMaxDelay(100*time.Millisecond))
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 100*time.Millisecond, d)
}

// TestCapDelay_ZeroMaxDelayMeansNoCap 验证 maxDelay <= 0 表示不限制上限，
// 而不是把所有延迟截断为 0（WithMaxDelay(0) 的可达路径）。
func TestCapDelay_ZeroMaxDelayMeansNoCap(t *testing.T) {
	assert.Equal(t, 5*time.Second, capDelay(5*time.Second, 0, false))
	assert.Equal(t, 5*time.Second, capDelay(5*time.Second, -1, false))
	// 设置了上限时仍正常截断
	assert.Equal(t, time.Second, capDelay(5*time.Second, time.Second, false))
}

// TestComputeDelay_NoMaxDelay 验证 WithMaxDelay(0) 下指数退避正常增长且不产生负值。
func TestComputeDelay_NoMaxDelay(t *testing.T) {
	c := Config{Delay: 100 * time.Millisecond, MaxDelay: 0, RetryIf: func(error) bool { return true }}
	assert.Equal(t, 100*time.Millisecond, computeDelay(c, 1, 0))
	assert.Equal(t, 200*time.Millisecond, computeDelay(c, 2, 0))
	// attempt 极大时位移溢出，应饱和为正值而不是回绕成 0 或负数
	assert.Positive(t, computeDelay(c, 200, 0))
}
