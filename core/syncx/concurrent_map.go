package syncx

import (
	"sync"
)

// ConcurrentMap is a generic concurrency-safe map.
// It uses sharded locks (buckets derived from the key hash), which scales better
// under concurrency than a single global lock.
//
// Use cases:
//   - caches with heavy concurrent reads and writes
//   - state shared between goroutines
//
// Usage:
//
//	m := syncx.NewConcurrentMap[string, int]()
//	m.Set("a", 1)
//	v, ok := m.Get("a")  // 1, true
//	m.Delete("a")
type ConcurrentMap[K comparable, V any] struct {
	shards []*mapShard[K, V]
	size   int
}

// mapShard is a single shard guarded by its own lock; each shard owns a subset
// of the keys.
type mapShard[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]V
}

const defaultShardCount = 32

// NewConcurrentMap creates a ConcurrentMap with the default 32 shards.
func NewConcurrentMap[K comparable, V any]() *ConcurrentMap[K, V] {
	return NewConcurrentMapWithSize[K, V](defaultShardCount)
}

// NewConcurrentMapWithSize creates a ConcurrentMap with shardCount shards.
// shardCount should be a power of two and is clamped to a minimum of 1.
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

// getShard returns the shard that owns key, derived from the key's hash.
func (m *ConcurrentMap[K, V]) getShard(key K) *mapShard[K, V] {
	// Use the built-in hash (going through any avoids generic hashing issues)
	h := hashKey(key)
	return m.shards[h%uint64(m.size)]
}

// Set stores a key/value pair.
func (m *ConcurrentMap[K, V]) Set(key K, value V) {
	shard := m.getShard(key)
	shard.mu.Lock()
	shard.items[key] = value
	shard.mu.Unlock()
}

// Get returns the value for key plus a flag reporting whether it exists.
func (m *ConcurrentMap[K, V]) Get(key K) (V, bool) {
	shard := m.getShard(key)
	shard.mu.RLock()
	val, ok := shard.items[key]
	shard.mu.RUnlock()
	return val, ok
}

// GetOrSet returns the value for key, storing defaultValue first when absent.
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

// GetAndDelete returns the value for key and removes it, plus an existence flag.
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

// Delete removes key; it is a no-op when the key does not exist.
func (m *ConcurrentMap[K, V]) Delete(key K) {
	shard := m.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

// Has reports whether key exists.
func (m *ConcurrentMap[K, V]) Has(key K) bool {
	shard := m.getShard(key)
	shard.mu.RLock()
	_, ok := shard.items[key]
	shard.mu.RUnlock()
	return ok
}

// Len returns the number of key/value pairs in the map.
func (m *ConcurrentMap[K, V]) Len() int {
	var count int
	for _, shard := range m.shards {
		shard.mu.RLock()
		count += len(shard.items)
		shard.mu.RUnlock()
	}
	return count
}

// Range iterates over every key/value pair.
// Iteration stops as soon as fn returns false.
// Each shard's read lock is held while that shard is traversed, so no writes
// should be performed during the traversal.
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

// Clear removes every key/value pair.
func (m *ConcurrentMap[K, V]) Clear() {
	for _, shard := range m.shards {
		shard.mu.Lock()
		shard.items = make(map[K]V)
		shard.mu.Unlock()
	}
}

// Keys returns a slice holding every key.
func (m *ConcurrentMap[K, V]) Keys() []K {
	var keys []K
	m.Range(func(key K, _ V) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

// Values returns a slice holding every value.
func (m *ConcurrentMap[K, V]) Values() []V {
	var values []V
	m.Range(func(_ K, value V) bool {
		values = append(values, value)
		return true
	})
	return values
}
