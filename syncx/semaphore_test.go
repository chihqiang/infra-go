package syncx

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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

func TestSemaphore_InvalidCapacity(t *testing.T) {
	// 非正数容量回退为 1
	for _, n := range []int{0, -1, -100} {
		sem := NewSemaphore(n)
		assert.Equal(t, 1, sem.Capacity(), "NewSemaphore(%d) capacity", n)

		sem.Acquire()
		assert.Equal(t, 0, sem.Available())
		sem.Release()
	}
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
