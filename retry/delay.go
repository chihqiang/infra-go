package retry

import (
	"math"
	"math/rand/v2"
	"time"
)

// --- 延迟计算 ---

// computeDelay 计算重试延迟。
func computeDelay(c Config, attempt int, previousDelay time.Duration) time.Duration {
	// 如果有自定义延迟函数，使用它
	if c.DelayFunc != nil {
		d := c.DelayFunc(attempt, previousDelay)
		return capDelay(d, c.MaxDelay, c.Jitter)
	}

	// 默认指数退避：delay * 2^(attempt-1)，用位运算替代浮点指数计算。
	d := c.Delay << uint(attempt-1)
	// 位移溢出（attempt 过大）时饱和，避免产生异常值。
	if d < c.Delay {
		d = c.MaxDelay
		if d <= 0 {
			// 未设置延迟上限（WithMaxDelay(0)）：饱和到 time.Duration 最大值，
			// 否则回绕后的负值会被 capDelay 归零，退化成无间隔重试。
			d = time.Duration(1<<63 - 1)
		}
	}
	return capDelay(d, c.MaxDelay, c.Jitter)
}

// capDelay 限制延迟不超过最大值，并可选添加抖动。
// maxDelay <= 0 表示不限制上限——normalize 会填充默认值，
// 只有通过 WithMaxDelay(0) 显式设置时才可能为 0。
func capDelay(d, maxDelay time.Duration, jitter bool) time.Duration {
	if maxDelay > 0 && d > maxDelay {
		d = maxDelay
	}
	if d < 0 {
		d = 0
	}
	if jitter && d > 0 {
		// 添加 0~50% 的随机抖动
		half := int64(d) / 2
		if half > 0 {
			jitterAmount := time.Duration(rand.Int64N(half))
			d += jitterAmount
			if maxDelay > 0 && d > maxDelay {
				d = maxDelay
			}
		}
	}
	return d
}

// --- 延迟策略 ---

// ExponentialBackoff 指数退避延迟。
// base 为基础延迟，factor 为乘数因子，attempt 为当前重试次数。
func ExponentialBackoff(base time.Duration, factor float64) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return time.Duration(float64(base) * math.Pow(factor, float64(attempt-1)))
	}
}

// FixedDelay 固定延迟。
func FixedDelay(delay time.Duration) DelayFunc {
	return func(_ int, _ time.Duration) time.Duration {
		return delay
	}
}

// LinearDelay 线性增长延迟。
// base 为基础延迟，increment 为每次重试的增加量。
func LinearDelay(base, increment time.Duration) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return base + time.Duration(attempt-1)*increment
	}
}
