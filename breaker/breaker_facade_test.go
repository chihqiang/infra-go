package breaker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件聚焦公开门面（circuitBreaker/NewBreaker）、包级便捷函数（breakers.go）
// 与 nopBreaker 全 API 的覆盖：这些路径原先大量 0%。
// 复用 breaker_test.go 中的白盒 helper：getTestGoogleBreaker/markFailed/verify。

// trippedGoogleBreaker 构造一个已处于打开状态（拒绝请求）的内部 googleBreaker。
func trippedGoogleBreaker(t *testing.T) *googleBreaker {
	t.Helper()
	b := getTestGoogleBreaker()
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool { return b.accept() != nil })
	return b
}

// facadeWithThrottle 构造公开门面，但将底层算法替换为 inner，便于精确控制状态。
func facadeWithThrottle(t *testing.T, inner internalThrottle) *circuitBreaker {
	t.Helper()
	cb := NewBreaker(WithName("facade")).(*circuitBreaker)
	cb.throttle = newLoggedThrottle(cb.name, inner)
	return cb
}

// TestFacade_AllowAndPromise 覆盖公开 Allow() 成功路径、Promise 的 Accept/Reject
// 以及 errorWindow 记录（promiseWithReason.Reject → errorWindow.add）。
func TestFacade_AllowAndPromise(t *testing.T) {
	b := NewBreaker(WithName("facade-promise"))

	// Accept：不记录失败原因
	promise, err := b.Allow()
	require.NoError(t, err)
	promise.Accept()

	// Reject(reason)：记录最近失败原因
	promise, err = b.Allow()
	require.NoError(t, err)
	promise.Reject("downstream timeout")

	// errorWindow 应已记录该原因（白盒断言）
	cb := b.(*circuitBreaker)
	ew := cb.throttle.(*loggedThrottle).errWin
	assert.Contains(t, ew.String(), "downstream timeout")
}

// TestFacade_CtxNormalVariants 覆盖各公开 Ctx 方法的正常（default）分支：
// 现有测试只覆盖了 ctx 取消分支，这里补正常放行路径。
func TestFacade_CtxNormalVariants(t *testing.T) {
	b := NewBreaker(WithName("facade-ctx-normal"))
	ctx := context.Background()

	// AllowCtx 正常
	promise, err := b.AllowCtx(ctx)
	require.NoError(t, err)
	promise.Accept()

	// DoCtx 正常
	require.NoError(t, b.DoCtx(ctx, func() error { return nil }))

	// DoWithAcceptableCtx 正常：业务错误被 acceptable 接受，不计失败
	boom := errors.New("business error")
	err = b.DoWithAcceptableCtx(ctx, func() error { return boom }, func(err error) bool {
		return errors.Is(err, boom)
	})
	assert.ErrorIs(t, err, boom)
	// 大量被接受错误后熔断仍关闭
	assert.NoError(t, b.DoCtx(ctx, func() error { return nil }))
}

// TestFacade_OpenState 覆盖公开 Allow/Do 在熔断打开时返回
// ErrServiceUnavailable，并触发 loggedThrottle.logError 完整分支（输出 errorWindow.String）。
func TestFacade_OpenState(t *testing.T) {
	b := facadeWithThrottle(t, trippedGoogleBreaker(t))

	_, err := b.Allow()
	assert.ErrorIs(t, err, ErrServiceUnavailable)

	err = b.Do(func() error { return nil })
	assert.ErrorIs(t, err, ErrServiceUnavailable)

	err = b.DoWithAcceptable(func() error { return nil }, defaultAcceptable)
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

// TestFacade_FallbackVariantsWhenOpen 覆盖各 Fallback 变体在熔断打开时执行降级。
func TestFacade_FallbackVariantsWhenOpen(t *testing.T) {
	fbErr := errors.New("fallback-result")

	t.Run("DoWithFallback", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallback(func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			})
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackCtx", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackCtx(context.Background(),
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			})
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackAcceptable", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackAcceptable(
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			},
			func(err error) bool { return false })
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackAcceptableCtx", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackAcceptableCtx(context.Background(),
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			},
			func(err error) bool { return false })
		assert.ErrorIs(t, err, fbErr)
	})
}

// TestFacade_FallbackIgnoredWhenClosed 覆盖熔断关闭时 fallback 不介入，
// 请求错误原样返回（仅 acceptable 决定是否计入失败）。
func TestFacade_FallbackIgnoredWhenClosed(t *testing.T) {
	b := facadeWithThrottle(t, getTestGoogleBreaker())
	boom := errors.New("boom")

	err := b.DoWithFallback(func() error { return boom },
		func(error) error {
			t.Fatal("fallback must not run when breaker is closed")
			return nil
		})
	assert.ErrorIs(t, err, boom)
}

// TestFacade_CtxCancelled 覆盖三个公开 Ctx 变体的 context 取消分支
// （DoWithAcceptableCtx/DoWithFallbackCtx/DoWithFallbackAcceptableCtx）。
func TestFacade_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := NewBreaker(WithName("facade-ctx-cancel"))

	err := b.DoWithAcceptableCtx(ctx, func() error { return nil }, defaultAcceptable)
	assert.Error(t, err)

	err = b.DoWithFallbackCtx(ctx, func() error { return nil }, func(error) error { return nil })
	assert.Error(t, err)

	err = b.DoWithFallbackAcceptableCtx(ctx, func() error { return nil },
		func(error) error { return nil }, defaultAcceptable)
	assert.Error(t, err)
}

// TestNopBreakerFullAPI 覆盖 nopBreaker 除 Do/Allow 外的全部方法
// 以及 nopPromise 的 Accept/Reject（原先均 0%）。
func TestNopBreakerFullAPI(t *testing.T) {
	b := NopBreaker()
	ctx := context.Background()

	// AllowCtx 返回 nopPromise
	promise, err := b.AllowCtx(ctx)
	require.NoError(t, err)
	promise.Accept()
	promise.Reject("ignored")

	// 各 Do 变体：直接执行 req，忽略 acceptable/fallback，panic 也不熔断
	called := 0
	req := func() error { called++; return nil }
	assert.NoError(t, b.DoCtx(ctx, req))
	assert.NoError(t, b.DoWithAcceptable(req, func(error) bool { return false }))
	assert.NoError(t, b.DoWithAcceptableCtx(ctx, req, func(error) bool { return false }))
	assert.NoError(t, b.DoWithFallback(req, func(error) error { return errors.New("fb") }))
	assert.NoError(t, b.DoWithFallbackCtx(ctx, req, func(error) error { return errors.New("fb") }))
	assert.NoError(t, b.DoWithFallbackAcceptable(req, func(error) error { return errors.New("fb") },
		func(error) bool { return false }))
	assert.NoError(t, b.DoWithFallbackAcceptableCtx(ctx, req,
		func(error) error { return errors.New("fb") }, func(error) bool { return false }))
	assert.Equal(t, 7, called, "每个变体都应执行一次 req")

	// nop 不做降级：req 失败时原样返回，fallback 不生效
	boom := errors.New("boom")
	err = b.DoWithFallback(func() error { return boom }, func(error) error { return nil })
	assert.ErrorIs(t, err, boom)
}

// TestGlobalBreakerHelpers 覆盖 breakers.go 的包级便捷函数（Do 已在既有测试覆盖，
// 这里补其余 7 个变体）。通过 NoBreakerFor 注册 nop，行为确定且不依赖时间窗口。
func TestGlobalBreakerHelpers(t *testing.T) {
	name := fmt.Sprintf("global-helpers-%d", time.Now().UnixNano())
	NoBreakerFor(name)
	ctx := context.Background()
	boom := errors.New("boom")

	// DoCtx
	var called int
	assert.NoError(t, DoCtx(ctx, name, func() error { called++; return nil }))
	assert.Equal(t, 1, called)

	// DoWithAcceptable / DoWithAcceptableCtx：返回 req 实际错误（可接受也照常返回）
	for _, fn := range []func() error{
		func() error {
			return DoWithAcceptable(name, func() error { return boom }, func(error) bool { return true })
		},
		func() error {
			return DoWithAcceptableCtx(ctx, name, func() error { return boom }, func(error) bool { return true })
		},
	} {
		assert.ErrorIs(t, fn(), boom)
	}

	// DoWithFallback 系列：nop 忽略 fallback，错误原样返回
	fbRun := false
	assert.ErrorIs(t, DoWithFallback(name, func() error { return boom },
		func(error) error { fbRun = true; return nil }), boom)
	assert.ErrorIs(t, DoWithFallbackCtx(ctx, name, func() error { return boom },
		func(error) error { fbRun = true; return nil }), boom)
	assert.ErrorIs(t, DoWithFallbackAcceptable(name, func() error { return boom },
		func(error) error { fbRun = true; return nil }, func(error) bool { return false }), boom)
	assert.ErrorIs(t, DoWithFallbackAcceptableCtx(ctx, name, func() error { return boom },
		func(error) error { fbRun = true; return nil }, func(error) bool { return false }), boom)
	assert.False(t, fbRun, "nop 不执行 fallback")
}
