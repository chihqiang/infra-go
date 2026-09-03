package syncx

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestConcurrentMap_Empty(t *testing.T) {
	m := NewConcurrentMap[string, int]()
	assert.Equal(t, 0, m.Len())
	assert.Empty(t, m.Keys())
	assert.Empty(t, m.Values())
}

func TestConcurrentMap_WithSize(t *testing.T) {
	m := NewConcurrentMapWithSize[string, int](1)
	m.Set("a", 1)
	assert.Equal(t, 1, m.Len())

	val, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, val)
}

func TestConcurrentMap_WithSize_Invalid(t *testing.T) {
	// shardCount < 1 时回退为 1
	m := NewConcurrentMapWithSize[string, int](0)
	m.Set("a", 1)
	assert.Equal(t, 1, m.Len())
	val, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	mNeg := NewConcurrentMapWithSize[string, int](-3)
	mNeg.Set("b", 2)
	assert.Equal(t, 1, mNeg.Len())
}

func TestConcurrentMap_StructKey(t *testing.T) {
	type key struct {
		A int
		B string
	}
	m := NewConcurrentMap[key, int]()
	m.Set(key{A: 1, B: "x"}, 10)

	val, ok := m.Get(key{A: 1, B: "x"})
	assert.True(t, ok)
	assert.Equal(t, 10, val)
}
