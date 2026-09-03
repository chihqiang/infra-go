package syncx

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestOnceValue_Panic(t *testing.T) {
	ov := NewOnceValue(func() int {
		panic("load failed")
	})

	// panic 应传播给调用方，而不是静默返回零值
	for i := 0; i < 2; i++ {
		func() {
			defer func() {
				r := recover()
				assert.Equal(t, "load failed", r)
			}()
			_ = ov.Get()
			t.Fatal("expected panic from Get")
		}()
	}
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

func TestOnceError_Panic(t *testing.T) {
	oe := NewOnceError(func() (string, error) {
		panic("init failed")
	})

	for i := 0; i < 2; i++ {
		func() {
			defer func() {
				r := recover()
				assert.Equal(t, "init failed", r)
			}()
			_, _ = oe.Get()
			t.Fatal("expected panic from Get")
		}()
	}
}
