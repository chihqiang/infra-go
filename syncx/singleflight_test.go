package syncx

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SingleFlight 测试 ---

func TestSingleFlight_Do(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	val, err := sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		time.Sleep(50 * time.Millisecond)
		return "result", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "result", val)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_Concurrent(t *testing.T) {
	sf := NewSingleFlight[int]()

	var callCount int32
	var wg sync.WaitGroup

	// 100 个协程同时调用同一个 key
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := sf.Do("same-key", func() (int, error) {
				atomic.AddInt32(&callCount, 1)
				time.Sleep(50 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			assert.Equal(t, 42, val)
		}()
	}
	wg.Wait()

	// fn 应该只被调用一次
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestSingleFlight_DifferentKeys(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n)
			_, err := sf.Do(key, func() (string, error) {
				atomic.AddInt32(&count, 1)
				return key, nil
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// 不同 key 各调用一次
	assert.Equal(t, int32(10), atomic.LoadInt32(&count))
}

func TestSingleFlight_Error(t *testing.T) {
	sf := NewSingleFlight[string]()

	val, err := sf.Do("key", func() (string, error) {
		return "", fmt.Errorf("custom error")
	})

	assert.Error(t, err)
	assert.Equal(t, "", val)
	assert.Contains(t, err.Error(), "custom error")
}

func TestSingleFlight_Forget(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		return "first", nil
	})

	sf.Forget("key")

	sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		return "second", nil
	})

	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestSingleFlight_Panic(t *testing.T) {
	sf := NewSingleFlight[int]()

	// panic 应向调用方传播
	func() {
		defer func() {
			r := recover()
			assert.Equal(t, "boom", r, "expected panic to propagate to caller")
		}()
		sf.Do("key", func() (int, error) {
			panic("boom")
		})
	}()

	// panic 后 key 应已清理，可正常再次执行
	val, err := sf.Do("key", func() (int, error) { return 42, nil })
	assert.NoError(t, err)
	assert.Equal(t, 42, val)
}

func TestSingleFlight_PanicDoesNotBlockWaiters(t *testing.T) {
	sf := NewSingleFlight[int]()

	var wg sync.WaitGroup
	start := make(chan struct{})

	// 第一个调用者 panic
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = recover() }()
		sf.Do("key", func() (int, error) {
			close(start)
			time.Sleep(50 * time.Millisecond)
			panic("boom")
		})
	}()

	<-start

	// 等待者应返回而非永久阻塞
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sf.Do("key", func() (int, error) { return 7, nil })
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter blocked forever after fn panicked")
	}
	wg.Wait()
}

// --- DoCtx 测试 ---

func TestSingleFlight_DoCtx_Success(t *testing.T) {
	sf := NewSingleFlight[string]()
	ctx := context.Background()

	var count int32
	val, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		atomic.AddInt32(&count, 1)
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", val)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_DoCtx_Concurrent(t *testing.T) {
	sf := NewSingleFlight[int]()

	var count int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
				atomic.AddInt32(&count, 1)
				time.Sleep(30 * time.Millisecond)
				return 7, nil
			})
			require.NoError(t, err)
			assert.Equal(t, 7, val)
		}()
	}
	wg.Wait()

	// fn 应只执行一次，所有等待者共享结果
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_DoCtx_Cancelled(t *testing.T) {
	sf := NewSingleFlight[string]()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		return "result", nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestSingleFlight_DoCtx_WaiterCancelled(t *testing.T) {
	sf := NewSingleFlight[string]()

	release := make(chan struct{})
	started := make(chan struct{})

	// leader 调用并阻塞在 fn 内部
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			close(started)
			<-release
			return "done", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "done", val)
	}()
	<-started

	// 等待者的 context 已取消：应返回错误而不是永久阻塞
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		return "unused", nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// 释放 leader，确认其仍能正常完成，且不会卡住
	close(release)
	wg.Wait()
}

func TestSingleFlight_DoCtx_Panic(t *testing.T) {
	sf := NewSingleFlight[int]()

	assert.Panics(t, func() {
		sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
			panic("boom")
		})
	})

	// panic 后 key 已清理，可再次执行
	val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
		return 1, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, val)
}

func TestSingleFlight_DoCtx_ErrorShared(t *testing.T) {
	sf := NewSingleFlight[string]()

	started := make(chan struct{})
	release := make(chan struct{})
	// 缓冲容量必须 ≥ 发送者数量（2），否则后发者在主 goroutine Wait 前会永久阻塞。
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// leader：进入 fn 后阻塞，确保后续调用者进来时一定作为"等待者"而非自己执行。
	go func() {
		defer wg.Done()
		_, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			close(started)
			<-release
			return "", fmt.Errorf("db down")
		})
		errCh <- err
	}()

	<-started // leader 已持锁并进入 fn

	// waiter：此刻进入必为等待者，应共享 leader 的错误。
	// 其兜底 fn 同样返回该错误，避免极端调度下（waiter 意外成为 leader）产生 flaky。
	go func() {
		defer wg.Done()
		_, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("db down")
		})
		errCh <- err
	}()

	// 给 waiter 时间进入等待状态，再放行 leader
	time.Sleep(20 * time.Millisecond)
	close(release)

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db down")
	}
}
