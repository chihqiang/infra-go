package syncx

import (
	"sync"
)

// ConcurrentMap 泛型并发安全 Map。
// 使用分段锁（基于 hash 分桶），比全局单一锁有更好的并发性能。
//
// 适用场景：
//   - 高并发读写缓存
//   - 协程间共享状态
//
// 用法：
//
//	m := syncx.NewConcurrentMap[string, int]()
//	m.Set("a", 1)
//	v, ok := m.Get("a")  // 1, true
//	m.Delete("a")
type ConcurrentMap[K comparable, V any] struct {
	shards []*mapShard[K, V]
	size   int
}

// mapShard 分段锁，每个 shard 负责一部分 key。
type mapShard[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]V
}

const defaultShardCount = 32

// NewConcurrentMap 创建一个默认 32 个分段的 ConcurrentMap。
func NewConcurrentMap[K comparable, V any]() *ConcurrentMap[K, V] {
	return NewConcurrentMapWithSize[K, V](defaultShardCount)
}

// NewConcurrentMapWithSize 创建指定分段数量的 ConcurrentMap。
// shardCount 建议为 2 的幂次，最少为 1。
func NewConcurrentMapWithSize[K comparable, V any](shardCount int) *ConcurrentMap[K, V] {
	if shardCount < 1 {
		shardCount = 1
	}
	m := &ConcurrentMap[K, V]{
		shards: make([]*mapShard[K, V], shardCount),
		size:   shardCount,
	}
	for i := range m.shards {
		m.shards[i] = &mapShard[K, V]{
			items: make(map[K]V),
		}
	}
	return m
}

// getShard 根据 key 的 hash 获取对应的分段。
func (m *ConcurrentMap[K, V]) getShard(key K) *mapShard[K, V] {
	// 使用 Go 内置 hash（通过 any 类型转换避免泛型 hash 问题）
	h := hashKey(key)
	return m.shards[h%uint64(m.size)]
}

// Set 设置键值对。
func (m *ConcurrentMap[K, V]) Set(key K, value V) {
	shard := m.getShard(key)
	shard.mu.Lock()
	shard.items[key] = value
	shard.mu.Unlock()
}

// Get 获取键对应的值，返回值和是否存在标志。
func (m *ConcurrentMap[K, V]) Get(key K) (V, bool) {
	shard := m.getShard(key)
	shard.mu.RLock()
	val, ok := shard.items[key]
	shard.mu.RUnlock()
	return val, ok
}

// GetOrSet 获取值，如果不存在则设置默认值后返回。
func (m *ConcurrentMap[K, V]) GetOrSet(key K, defaultValue V) (V, bool) {
	shard := m.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if val, ok := shard.items[key]; ok {
		return val, true
	}
	shard.items[key] = defaultValue
	return defaultValue, false
}

// GetAndDelete 获取并删除值，返回值和是否存在标志。
func (m *ConcurrentMap[K, V]) GetAndDelete(key K) (V, bool) {
	shard := m.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	val, ok := shard.items[key]
	if ok {
		delete(shard.items, key)
	}
	return val, ok
}

// Delete 删除键，如果键不存在则什么也不做。
func (m *ConcurrentMap[K, V]) Delete(key K) {
	shard := m.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

// Has 检查键是否存在。
func (m *ConcurrentMap[K, V]) Has(key K) bool {
	shard := m.getShard(key)
	shard.mu.RLock()
	_, ok := shard.items[key]
	shard.mu.RUnlock()
	return ok
}

// Len 返回 map 中键值对的数量。
func (m *ConcurrentMap[K, V]) Len() int {
	var count int
	for _, shard := range m.shards {
		shard.mu.RLock()
		count += len(shard.items)
		shard.mu.RUnlock()
	}
	return count
}

// Range 遍历所有键值对。
// 如果 fn 返回 false，则停止遍历。
// 遍历期间会持有各分段的读锁，因此遍历期间不应执行写操作。
func (m *ConcurrentMap[K, V]) Range(fn func(key K, value V) bool) {
	for _, shard := range m.shards {
		shard.mu.RLock()
		for k, v := range shard.items {
			if !fn(k, v) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

// Clear 清空所有键值对。
func (m *ConcurrentMap[K, V]) Clear() {
	for _, shard := range m.shards {
		shard.mu.Lock()
		shard.items = make(map[K]V)
		shard.mu.Unlock()
	}
}

// Keys 返回所有键的切片。
func (m *ConcurrentMap[K, V]) Keys() []K {
	var keys []K
	m.Range(func(key K, _ V) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

// Values 返回所有值的切片。
func (m *ConcurrentMap[K, V]) Values() []V {
	var values []V
	m.Range(func(_ K, value V) bool {
		values = append(values, value)
		return true
	})
	return values
}
