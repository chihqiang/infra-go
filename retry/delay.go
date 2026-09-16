package retry

import (
	"math"
	"math/rand/v2"
	"time"
)

// --- Delay computation ---

// computeDelay computes the retry delay.
func computeDelay(c Config, attempt int, previousDelay time.Duration) time.Duration {
	// Use the custom delay function when one is set
	if c.DelayFunc != nil {
		d := c.DelayFunc(attempt, previousDelay)
		return capDelay(d, c.MaxDelay, c.Jitter)
	}

	// Default exponential backoff: delay * 2^(attempt-1), using a bit shift instead of a
	// floating point exponent.
	d := c.Delay << uint(attempt-1)
	// Saturate on a shift overflow (attempt too large) instead of producing a bogus value.
	if d < c.Delay {
		d = c.MaxDelay
		if d <= 0 {
			// No delay ceiling is set (WithMaxDelay(0)): saturate to the maximum
			// time.Duration, otherwise the wrapped negative value would be zeroed by
			// capDelay and degenerate into retrying with no interval at all.
			d = time.Duration(1<<63 - 1)
		}
	}
	return capDelay(d, c.MaxDelay, c.Jitter)
}

// capDelay limits the delay to the maximum value and optionally adds jitter.
// maxDelay <= 0 means no ceiling - normalize fills in the default, so it can only be 0 when
// WithMaxDelay(0) set it explicitly.
func capDelay(d, maxDelay time.Duration, jitter bool) time.Duration {
	if maxDelay > 0 && d > maxDelay {
		d = maxDelay
	}
	if d < 0 {
		d = 0
	}
	if jitter && d > 0 {
		// Add a random 0~50% jitter
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

// --- Delay strategies ---

// ExponentialBackoff returns an exponential backoff delay.
// base is the base delay, factor is the multiplier and attempt is the current retry number.
func ExponentialBackoff(base time.Duration, factor float64) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return time.Duration(float64(base) * math.Pow(factor, float64(attempt-1)))
	}
}

// FixedDelay returns a fixed delay.
func FixedDelay(delay time.Duration) DelayFunc {
	return func(_ int, _ time.Duration) time.Duration {
		return delay
	}
}

// LinearDelay returns a linearly growing delay.
// base is the base delay and increment is the amount added on every retry.
func LinearDelay(base, increment time.Duration) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return base + time.Duration(attempt-1)*increment
	}
}
