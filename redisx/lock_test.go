package redisx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMiniRedisClient 创建基于 miniredis 的客户端。
func newMiniRedisClient(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := New(Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
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
	c, _ := newMiniRedisClient(t)
	la := c.Locker("test-lock", 10*time.Second)
	assert.NotNil(t, la)
}

// --- TryLock ---

func TestTryLock_Success(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)
	assert.NotNil(t, lock)

	// Redis 中应存在锁键
	val, err := mr.Get("lock")
	require.NoError(t, err)
	assert.NotEmpty(t, val)

	// 释放后键被删除
	require.NoError(t, lock.Unlock(ctx))
	assert.False(t, mr.Exists("lock"))
}

func TestTryLock_Conflict(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(ctx)
	require.NoError(t, err)
	defer lock1.Unlock(ctx)

	// 第二个获取同一把锁应失败
	_, err = la.TryLock(ctx)
	assert.ErrorIs(t, err, ErrLockNotAcquired)
	assert.True(t, IsLockNotAcquired(err))
}

func TestTryLock_ContextCancelled(t *testing.T) {
	c, _ := newMiniRedisClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Locker("lock", time.Second).TryLock(ctx)
	assert.Error(t, err)
}

// --- Lock（阻塞式） ---

func TestLock_BlockUntilReleased(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(ctx)
	require.NoError(t, err)

	// 并发阻塞获取
	var (
		got    *Lock
		gotErr error
		wg     sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, gotErr = la.Lock(ctx, 10*time.Millisecond)
	}()

	// 确保阻塞获取已开始尝试
	time.Sleep(50 * time.Millisecond)

	// 释放第一把锁
	require.NoError(t, lock1.Unlock(ctx))

	// 等 goroutine 拿到锁
	wg.Wait()
	require.NoError(t, gotErr)
	require.NotNil(t, got)
	defer got.Unlock(ctx)
}

func TestLock_ContextCancelledWhileWaiting(t *testing.T) {
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(context.Background())
	require.NoError(t, err)
	defer lock1.Unlock(context.Background())

	// 取消的 ctx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = la.Lock(ctx, 5*time.Millisecond)
	assert.Error(t, err)
}

// --- Unlock ---

func TestUnlock_OwnershipMismatch(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)

	// 第三方直接删除锁键，导致 token 不匹配
	mr.Del("lock")

	err = lock.Unlock(ctx)
	assert.ErrorIs(t, err, ErrLockOwnershipMismatch)
}

func TestUnlock_Twice(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)

	require.NoError(t, lock.Unlock(ctx))
	// 第二次 Unlock：键已删除，token 不匹配
	assert.ErrorIs(t, lock.Unlock(ctx), ErrLockOwnershipMismatch)
}

// --- SetNXWithLock ---

func TestSetNXWithLock_Success(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	var called int32
	err := c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))

	// 锁应已被释放，可再次获取
	lock, err := c.Locker("lock", 5*time.Second).TryLock(ctx)
	require.NoError(t, err)
	lock.Unlock(ctx)
}

func TestSetNXWithLock_NotAcquired(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	// 先占住锁
	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)
	defer lock.Unlock(ctx)

	var called int32
	err = c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	assert.ErrorIs(t, err, ErrLockNotAcquired)
	assert.Equal(t, int32(0), atomic.LoadInt32(&called))
}

func TestSetNXWithLock_FnError(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	sentinel := errors.New("fn error")
	err := c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		return sentinel
	})
	assert.Same(t, sentinel, err)

	// 即使 fn 报错，锁也应被释放
	lock, err := c.Locker("lock", 5*time.Second).TryLock(context.Background())
	require.NoError(t, err)
	lock.Unlock(context.Background())
}

// --- 自动续期 ---

func TestLock_AutoRenew(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	// 短 TTL + 自动续期（续期周期 = ttl/3）
	lock, err := c.Locker("lock", 300*time.Millisecond, WithAutoRenew()).TryLock(ctx)
	require.NoError(t, err)

	// 循环：真实 sleep 让 renewLoop 执行续期（重置 TTL），再 FastForward 推进虚拟时钟。
	// 若无续期，累计快进超过 300ms 后锁必过期；有续期则始终存活。
	for i := 0; i < 5; i++ {
		time.Sleep(150 * time.Millisecond) // 让 renewLoop 至少续期一次
		mr.FastForward(200 * time.Millisecond)
		assert.True(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should be renewed (iter %d)", i)
	}

	// 释放后续期停止，锁应过期
	require.NoError(t, lock.Unlock(ctx))
	mr.FastForward(500 * time.Millisecond)
	assert.False(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should expire after unlock")
}

func TestLock_NoAutoRenew_Expires(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	// 短 TTL、不续期
	lock, err := c.Locker("lock", 100*time.Millisecond).TryLock(ctx)
	require.NoError(t, err)
	defer lock.Unlock(ctx)

	// FastForward 推进虚拟时钟使 TTL 过期
	mr.FastForward(300 * time.Millisecond)
	assert.False(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should expire without renewal")
}

// --- TTL 校验（防永不过期的锁 / 续期 panic / 续期时删锁） ---

func TestTryLock_InvalidTTLRejected(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	cases := []struct {
		name string
		ttl  time.Duration
	}{
		{"zero", 0},
		{"negative", -time.Second},
		{"sub_millisecond", time.Microsecond},
		{"500us", 500 * time.Microsecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Locker("lock", tc.ttl).TryLock(ctx)
			require.ErrorIs(t, err, ErrInvalidLockTTL)

			// 非法 TTL 不应写入任何 Redis key（尤其是"永不过期"的锁）
			assert.False(t, c.client.Exists(ctx, "lock").Val() > 0,
				"no lock key should be created for invalid ttl %v", tc.ttl)
		})
	}
	assert.Empty(t, mr.Keys(), "no key should be created for invalid ttls")
}

func TestTryLock_InvalidTTLViaOption(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	// Locker 传入合法 TTL，但被 WithTTL 覆盖为非法值 → 同样必须被拒绝
	_, err := c.Locker("lock", time.Second, WithTTL(0)).TryLock(ctx)
	require.ErrorIs(t, err, ErrInvalidLockTTL)

	_, err = c.Locker("lock", time.Second, WithTTL(-time.Millisecond)).TryLock(ctx)
	require.ErrorIs(t, err, ErrInvalidLockTTL)
}

// TestTryLock_InvalidTTLWithAutoRenewDoesNotPanic 验证非法 TTL + 自动续期
// 不会再触发 time.NewTicker(0) 的 goroutine panic（历史缺陷会导致进程退出）。
func TestTryLock_InvalidTTLWithAutoRenewDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	require.NotPanics(t, func() {
		_, err := c.Locker("lock", 0, WithAutoRenew()).TryLock(ctx)
		require.ErrorIs(t, err, ErrInvalidLockTTL)
	})

	// 留出时间窗口：若旧实现真的启动了 renewLoop，panic 会在此刻发生
	time.Sleep(50 * time.Millisecond)
}

// TestLock_InvalidTTLPropagatesThroughLock 验证阻塞式 Lock 也会快速失败，
// 而不是自旋重试到 ctx 超时。
func TestLock_InvalidTTLPropagatesThroughLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _ := newMiniRedisClient(t)

	start := time.Now()
	_, err := c.Locker("lock", 0).Lock(ctx, 10*time.Millisecond)
	require.ErrorIs(t, err, ErrInvalidLockTTL)
	assert.Less(t, time.Since(start), time.Second, "should fail fast, not spin until ctx timeout")
}

// TestSetNXWithLock_InvalidTTL 验证便捷方法同样受 TTL 校验保护。
func TestSetNXWithLock_InvalidTTL(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	executed := false
	err := c.SetNXWithLock(ctx, "lock", 0, func(context.Context) error {
		executed = true
		return nil
	})
	require.ErrorIs(t, err, ErrInvalidLockTTL)
	assert.False(t, executed, "critical section must not run when lock ttl is invalid")
}

func TestValidateLockTTL(t *testing.T) {
	assert.NoError(t, validateLockTTL(minLockTTL))
	assert.NoError(t, validateLockTTL(time.Second))

	assert.ErrorIs(t, validateLockTTL(0), ErrInvalidLockTTL)
	assert.ErrorIs(t, validateLockTTL(-time.Second), ErrInvalidLockTTL)
	assert.ErrorIs(t, validateLockTTL(minLockTTL-time.Nanosecond), ErrInvalidLockTTL)
}
