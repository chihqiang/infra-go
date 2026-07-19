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

// --- ConcurrentMap 测试 ---

func TestConcurrentMap_Basic(t *testing.T) {
	m := NewConcurrentMap[string, int]()

	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	val, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	_, ok = m.Get("not-exist")
	assert.False(t, ok)

	assert.True(t, m.Has("b"))
	assert.False(t, m.Has("z"))

	assert.Equal(t, 3, m.Len())
}

func TestConcurrentMap_Delete(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	m.Delete("a")
	assert.False(t, m.Has("a"))
	assert.True(t, m.Has("b"))
	assert.Equal(t, 1, m.Len())
}

func TestConcurrentMap_GetAndDelete(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 100)

	val, ok := m.GetAndDelete("a")
	assert.True(t, ok)
	assert.Equal(t, 100, val)
	assert.False(t, m.Has("a"))

	// 删除不存在的
	_, ok = m.GetAndDelete("not-exist")
	assert.False(t, ok)
}

func TestConcurrentMap_GetOrSet(t *testing.T) {
	m := NewConcurrentMap[string, int]()

	// 不存在时设置
	val, existed := m.GetOrSet("a", 1)
	assert.False(t, existed)
	assert.Equal(t, 1, val)

	// 已存在时返回现有值
	val, existed = m.GetOrSet("a", 999)
	assert.True(t, existed)
	assert.Equal(t, 1, val) // 返回旧值
}

func TestConcurrentMap_Concurrent(t *testing.T) {
	m := NewConcurrentMap[int, int]()

	var wg sync.WaitGroup
	// 100 个协程并发写
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Set(n, n*2)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, m.Len())

	// 验证值
	for i := 0; i < 100; i++ {
		val, ok := m.Get(i)
		assert.True(t, ok)
		assert.Equal(t, i*2, val)
	}
}

func TestConcurrentMap_Range(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var keys []string
	var values []int
	m.Range(func(key string, value int) bool {
		keys = append(keys, key)
		values = append(values, value)
		return true
	})

	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "a")
	assert.Contains(t, keys, "b")
	assert.Contains(t, keys, "c")
}

func TestConcurrentMap_RangeStop(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var count int
	m.Range(func(key string, value int) bool {
		count++
		return false // 立即停止
	})

	assert.Equal(t, 1, count)
}

func TestConcurrentMap_Clear(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	m.Clear()
	assert.Equal(t, 0, m.Len())
	assert.False(t, m.Has("a"))
}

func TestConcurrentMap_Keys_Values(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	keys := m.Keys()
	assert.Len(t, keys, 3)

	values := m.Values()
	assert.Len(t, values, 3)
	assert.Contains(t, values, 1)
	assert.Contains(t, values, 2)
	assert.Contains(t, values, 3)
}

func TestConcurrentMap_WithSize(t *testing.T) {
	m := NewConcurrentMapWithSize[string, int](1)
	m.Set("a", 1)
	assert.Equal(t, 1, m.Len())

	val, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, val)
}

// --- OnceValue 测试 ---

func TestOnceValue(t *testing.T) {
	var count int32
	ov := NewOnceValue(func() int {
		atomic.AddInt32(&count, 1)
		return 42
	})

	// 多次调用只执行一次
	assert.Equal(t, 42, ov.Get())
	assert.Equal(t, 42, ov.Get())
	assert.Equal(t, 42, ov.Get())
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestOnceValue_Concurrent(t *testing.T) {
	var count int32
	ov := NewOnceValue(func() string {
		atomic.AddInt32(&count, 1)
		time.Sleep(10 * time.Millisecond)
		return "loaded"
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.Equal(t, "loaded", ov.Get())
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

// --- OnceError 测试 ---

func TestOnceError(t *testing.T) {
	var count int32
	oe := NewOnceError(func() (string, error) {
		atomic.AddInt32(&count, 1)
		return "value", nil
	})

	val, err := oe.Get()
	require.NoError(t, err)
	assert.Equal(t, "value", val)

	val, err = oe.Get()
	require.NoError(t, err)
	assert.Equal(t, "value", val)

	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestOnceError_Error(t *testing.T) {
	oe := NewOnceError(func() (string, error) {
		return "", fmt.Errorf("load failed")
	})

	val, err := oe.Get()
	assert.Error(t, err)
	assert.Equal(t, "", val)
	assert.Contains(t, err.Error(), "load failed")
}

// --- Semaphore 测试 ---

func TestSemaphore_Basic(t *testing.T) {
	sem := NewSemaphore(3)
	assert.Equal(t, 3, sem.Capacity())
	assert.Equal(t, 3, sem.Available())

	sem.Acquire()
	assert.Equal(t, 2, sem.Available())

	sem.Acquire()
	sem.Acquire()
	assert.Equal(t, 0, sem.Available())

	sem.Release()
	assert.Equal(t, 1, sem.Available())

	sem.Release()
	sem.Release()
	assert.Equal(t, 3, sem.Available())
}

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := NewSemaphore(1)

	assert.True(t, sem.TryAcquire())
	assert.False(t, sem.TryAcquire()) // 已满

	sem.Release()
	assert.True(t, sem.TryAcquire())
}

func TestSemaphore_Concurrent(t *testing.T) {
	sem := NewSemaphore(5)
	var current int32
	var maxCurrent int32
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		sem.Acquire()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer sem.Release()

			cur := atomic.AddInt32(&current, 1)
			for {
				max := atomic.LoadInt32(&maxCurrent)
				if cur <= max || atomic.CompareAndSwapInt32(&maxCurrent, max, cur) {
					break
				}
			}
			time.Sleep(1 * time.Millisecond)
			atomic.AddInt32(&current, -1)
		}()
	}
	wg.Wait()

	// 并发数不应超过信号量容量
	assert.LessOrEqual(t, atomic.LoadInt32(&maxCurrent), int32(5))
}

func TestSemaphore_Wait(t *testing.T) {
	sem := NewSemaphore(2)
	var completed int32

	for i := 0; i < 10; i++ {
		sem.Acquire()
		go func() {
			defer sem.Release()
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
		}()
	}

	sem.Wait()
	assert.Equal(t, int32(10), atomic.LoadInt32(&completed))
}

// --- OrDone 测试 ---

func TestOrDone(t *testing.T) {
	src := make(chan int, 5)
	done := make(chan struct{})

	for i := 1; i <= 5; i++ {
		src <- i
	}
	close(src)

	var result []int
	for v := range OrDone(done, src) {
		result = append(result, v)
	}

	assert.Equal(t, []int{1, 2, 3, 4, 5}, result)
}

func TestOrDone_Cancelled(t *testing.T) {
	src := make(chan int)
	done := make(chan struct{})

	go func() {
		src <- 1
		time.Sleep(10 * time.Millisecond)
		close(done) // 取消
	}()

	var result []int
	for v := range OrDone(done, src) {
		result = append(result, v)
	}

	// 只收到第一个值就因 done 关闭而退出
	assert.Equal(t, []int{1}, result)
}

func TestOrDoneCtx(t *testing.T) {
	src := make(chan string, 3)
	ctx, cancel := context.WithCancel(context.Background())

	src <- "a"
	src <- "b"
	src <- "c"
	close(src)

	var result []string
	for v := range OrDoneCtx(ctx, src) {
		result = append(result, v)
	}
	cancel()

	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestOrDoneCtx_Cancelled(t *testing.T) {
	src := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		src <- 1
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	var result []int
	for v := range OrDoneCtx(ctx, src) {
		result = append(result, v)
	}

	assert.Equal(t, []int{1}, result)
}

// --- Merge 测试 ---

func TestMerge(t *testing.T) {
	ch1 := make(chan int, 3)
	ch2 := make(chan int, 3)
	ch3 := make(chan int, 3)

	ch1 <- 1
	ch1 <- 2
	ch1 <- 3
	close(ch1)

	ch2 <- 10
	ch2 <- 20
	close(ch2)

	ch3 <- 100
	close(ch3)

	ctx := context.Background()
	var result []int
	for v := range Merge(ctx, ch1, ch2, ch3) {
		result = append(result, v)
	}

	assert.Len(t, result, 6)
	assert.Contains(t, result, 1)
	assert.Contains(t, result, 100)
}

// --- FanOut 测试 ---

func TestFanOut(t *testing.T) {
	src := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		src <- i
	}
	close(src)

	ctx := context.Background()
	outs := FanOut(ctx, src, 3)

	var allValues []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, out := range outs {
		wg.Add(1)
		go func(ch <-chan int) {
			defer wg.Done()
			for v := range ch {
				mu.Lock()
				allValues = append(allValues, v)
				mu.Unlock()
			}
		}(out)
	}
	wg.Wait()

	// 每个 output channel 都应该收到全部 6 个值
	assert.Len(t, allValues, 18) // 3 outputs × 6 values
}
