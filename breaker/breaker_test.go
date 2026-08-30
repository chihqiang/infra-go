package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBuckets  = 10
	testInterval = time.Millisecond * 10
)

// getTestGoogleBreaker 构造使用短窗口的熔断器，加速测试。
func getTestGoogleBreaker() *googleBreaker {
	return &googleBreaker{
		k:          5,
		minK:       minK,
		stat:       newRollingWindow(testBuckets, testInterval),
		proba:      newProba(),
		lastPass:   newAtomicNano(),
		protection: protection,
	}
}

func markSuccess(b *googleBreaker, count int) {
	for i := 0; i < count; i++ {
		b.markSuccess()
	}
}

func markFailed(b *googleBreaker, count int) {
	for i := 0; i < count; i++ {
		b.markFailure()
	}
}

// verify 轮询断言，等待条件满足（熔断状态变化需要时间）。
func verify(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(testInterval)
	}
	t.Fatal("condition not met within deadline")
}

func TestGoogleBreakerClose(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 80)
	assert.NoError(t, b.accept())
	markSuccess(b, 120)
	assert.NoError(t, b.accept())
}

func TestGoogleBreakerOpen(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)
	assert.NoError(t, b.accept())
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})
}

func TestGoogleBreakerRecover(t *testing.T) {
	b := getTestGoogleBreaker()

	// 先制造大量失败触发熔断
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})

	// 连续成功后恢复
	markSuccess(b, 1000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() == nil
	})
}

func TestGoogleBreakerFallback(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 1)
	assert.NoError(t, b.accept())
	markFailed(b, 10000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		// 熔断打开时 fallback 生效，返回 nil
		return b.doReq(func() error {
			return errors.New("any")
		}, func(error) error {
			return nil
		}, defaultAcceptable) == nil
	})
}

func TestGoogleBreakerReject(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 100)
	assert.NoError(t, b.accept())
	markFailed(b, 10000)
	time.Sleep(testInterval)
	assert.ErrorIs(t, b.doReq(func() error {
		return ErrServiceUnavailable
	}, nil, defaultAcceptable), ErrServiceUnavailable)
}

func TestBreakerDoWithAcceptable(t *testing.T) {
	b := NewBreaker(WithName("test-ok"))

	// 业务错误被 acceptable 接受，不计入失败，不触发熔断
	// 注意：DoWithAcceptable 仍返回 req 的实际错误（acceptable 只决定是否计入失败）
	for i := 0; i < 100; i++ {
		err := b.DoWithAcceptable(func() error {
			return errNotFound
		}, func(err error) bool {
			return errors.Is(err, errNotFound)
		})
		assert.ErrorIs(t, err, errNotFound)
	}

	// 大量被接受的业务错误后，熔断器仍处于关闭状态
	assert.NoError(t, b.Do(func() error { return nil }))
}

func TestBreakerDoWithFallbackAcceptable(t *testing.T) {
	b := getTestGoogleBreaker()
	markFailed(b, 10000)
	time.Sleep(testInterval * 2)

	// 熔断打开：fallback 返回 nil
	err := b.doReq(func() error {
		return errors.New("upstream down")
	}, func(err error) error {
		return nil
	}, defaultAcceptable)
	assert.NoError(t, err)
}

func TestBreakerAllowPromise(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	promise, err := b.allow()
	require.NoError(t, err)
	promise.Accept()

	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		_, err := b.allow()
		return err != nil
	})
}

func TestBreakerPromiseReject(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	promise, err := b.allow()
	require.NoError(t, err)
	// Reject 路径：标记失败
	promise.Reject()

	// 大量失败后熔断打开
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		_, err := b.allow()
		return err != nil
	})
}

func TestAtomicNanoSetLoad(t *testing.T) {
	a := newAtomicNano()
	assert.Zero(t, a.Load())

	now := time.Now().UnixNano()
	a.Set(now)
	assert.Equal(t, now, a.Load())
}

func TestRollingWindowExpiredBuckets(t *testing.T) {
	rw := newRollingWindow(testBuckets, testInterval)

	// 第一个桶写入数据
	rw.add(success)

	// 等待窗口过期后，旧数据不应再被统计
	time.Sleep(testInterval * (testBuckets + 1))
	rw.add(fail)

	var result windowResult
	rw.reduce(func(b *bucket) {
		result.total += b.Sum
		result.accepts += b.Success
	})
	// 旧桶已过期被重置，只剩新写入的 fail
	assert.Equal(t, int64(1), result.total)
	assert.Equal(t, int64(0), result.accepts)
}

func TestBreakerAllowCtxCancelled(t *testing.T) {
	b := NewBreaker(WithName("test-ctx"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.AllowCtx(ctx)
	assert.Error(t, err)

	err = b.DoCtx(ctx, func() error { return nil })
	assert.Error(t, err)
}

func TestBreakerPanic(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	assert.Panics(t, func() {
		_ = b.doReq(func() error {
			panic("boom")
		}, nil, defaultAcceptable)
	})
}

func TestBreakerName(t *testing.T) {
	b := NewBreaker(WithName("payment"))
	assert.Equal(t, "payment", b.Name())

	// 未指定名称使用默认
	d := NewBreaker()
	assert.Equal(t, "breaker", d.Name())
}

func TestGetBreaker(t *testing.T) {
	b1 := GetBreaker("my-service")
	b2 := GetBreaker("my-service")
	assert.Same(t, b1, b2, "同名熔断器应共享实例")
	assert.Equal(t, "my-service", b1.Name())
}

func TestNoBreakerFor(t *testing.T) {
	NoBreakerFor("no-breaker")
	b := GetBreaker("no-breaker")
	assert.Equal(t, nopBreakerName, b.Name())

	// nopBreaker 永不熔断
	for i := 0; i < 100; i++ {
		_ = b.Do(func() error {
			return errors.New("always fail")
		})
	}
	assert.NoError(t, b.Do(func() error { return nil }))
}

func TestNopBreaker(t *testing.T) {
	b := NopBreaker()

	err := b.Do(func() error { return errors.New("x") })
	assert.Error(t, err)

	promise, err := b.Allow()
	assert.NoError(t, err)
	assert.NotNil(t, promise)
}

func TestDoGlobal(t *testing.T) {
	// 全局 Do 便捷函数
	var called bool
	err := Do("global-test", func() error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestRollingWindowReduce(t *testing.T) {
	rw := newRollingWindow(testBuckets, testInterval)

	rw.add(success)
	rw.add(success)
	rw.add(fail)

	var result windowResult
	rw.reduce(func(b *bucket) {
		result.accepts += b.Success
		result.total += b.Sum
	})
	assert.Equal(t, int64(2), result.accepts)
	assert.Equal(t, int64(3), result.total)
}

// errNotFound 用于 DoWithAcceptable 测试的哨兵业务错误。
var errNotFound = errors.New("not found")

// --- SRE 算法参数 Option 测试 ---

// sreOf 通过 NewBreaker 应用 opts 后，取出底层 googleBreaker 以便断言参数。
func sreOf(opts ...Option) *googleBreaker {
	return NewBreaker(opts...).(*circuitBreaker).throttle.(*loggedThrottle).internalThrottle.(*googleBreaker)
}

func TestDefaultSREConfig(t *testing.T) {
	cfg := defaultSREConfig()
	assert.Equal(t, window, cfg.window, "默认窗口应为 10s")
	assert.Equal(t, k, cfg.k, "默认 K 应为 1.5")
	assert.Equal(t, minK, cfg.minK, "默认 minK 应为 1.1")
	assert.Equal(t, int64(protection), cfg.protection, "默认 protection 应为 5")
}

func TestWithSREDefaults(t *testing.T) {
	b := sreOf(WithSREDefaults())
	assert.Equal(t, 2*time.Minute, b.stat.interval*time.Duration(b.stat.size), "SRE 默认窗口应为 2 分钟")
	assert.Equal(t, 2.0, b.k, "SRE 默认 K 应为 2")
	assert.Equal(t, 1.1, b.minK)
	assert.Equal(t, int64(5), b.protection)
}

func TestBreakerWithWindow(t *testing.T) {
	// 指定窗口：覆盖默认 10s
	b := sreOf(WithWindow(time.Minute))
	assert.Equal(t, time.Minute, b.stat.interval*time.Duration(b.stat.size))

	// 非法值被忽略，保留默认
	b2 := sreOf(WithWindow(0), WithWindow(-time.Second))
	assert.Equal(t, window, b2.stat.interval*time.Duration(b2.stat.size), "非正窗口应被忽略")
}

func TestBreakerWithK(t *testing.T) {
	b := sreOf(WithK(2))
	assert.Equal(t, 2.0, b.k)

	// 非法值被忽略
	b2 := sreOf(WithK(0), WithK(-1))
	assert.Equal(t, k, b2.k, "非正 K 应被忽略")
}

func TestBreakerWithMinK(t *testing.T) {
	b := sreOf(WithMinK(1.5))
	assert.Equal(t, 1.5, b.minK)

	b2 := sreOf(WithMinK(0))
	assert.Equal(t, minK, b2.minK, "非正 minK 应被忽略")
}

func TestBreakerWithProtection(t *testing.T) {
	b := sreOf(WithProtection(20))
	assert.Equal(t, int64(20), b.protection)

	// 负值被忽略，0 是合法值（关闭小流量保护）
	b2 := sreOf(WithProtection(-1))
	assert.Equal(t, int64(protection), b2.protection, "负 protection 应被忽略")
	b3 := sreOf(WithProtection(0))
	assert.Equal(t, int64(0), b3.protection, "0 protection 合法（不保护）")
}

// TestSREDefaultsStillTrips 验证 SRE 参数（2min/K=2）下熔断仍能正常打开与恢复。
// 使用短窗口近似：直接构造短窗口的 googleBreaker 但套用 SRE 的 K=2。
func TestSREDefaultsStillTrips(t *testing.T) {
	b := &googleBreaker{
		k:          2,
		minK:       minK,
		protection: 5,
		stat:       newRollingWindow(testBuckets, testInterval),
		proba:      newProba(),
		lastPass:   newAtomicNano(),
	}
	markSuccess(b, 10)
	assert.NoError(t, b.accept())
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})

	// 恢复
	markSuccess(b, 1000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() == nil
	})
}

// TestWithSREDefaultsEndToEnd 走公开 NewBreaker 接口验证 SRE 参数可正常创建。
func TestWithSREDefaultsEndToEnd(t *testing.T) {
	b := NewBreaker(WithName("sre"), WithSREDefaults())
	assert.NoError(t, b.Do(func() error { return nil }))
	assert.Equal(t, "sre", b.Name())
}
