package cache

import (
	"container/list"
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/syncx"
)

// Statistics and expiry parameters.
const (
	defaultCacheName = "cache"
	statInterval     = time.Minute
	// Expiry jitter: spreads the effective expiry over [0.95, 1.05] * expire so
	// that a large number of keys does not expire at the same instant
	// (cache-avalanche protection).
	expiryDeviation = 0.05
)

// cachedEntry is a cache entry.
// expireAt is the expiry instant; the zero value means the entry never expires (the
// case where expire <= 0 at construction time). Expiry is decided from expireAt:
// a hit in Get deletes lazily and a global scan cleans up the rest. No version
// number is needed: there are no timer callbacks, so the expireAt of the last write
// is the currently effective one.
type cachedEntry struct {
	value    any
	expireAt time.Time
}

// MemCache is the in-memory implementation of the Cache interface.
//
// Features:
//   - Expiry: lazy deletion (checked on a Get hit) plus a periodic global scan,
//     avoiding one timer per key
//   - LRU eviction: the least recently used key is evicted when capacity is limited
//   - Stampede protection: Take merges concurrent requests for the same key via SingleFlight
//   - Hit-ratio statistics: periodically logs QPS, hit ratio and element count
type MemCache struct {
	// lock guards data and lru. An RWMutex is used: without a capacity limit
	// (noLimit) the read path only needs the read lock and can run in parallel;
	// with LRU enabled a read hit updates the access order and needs the write lock.
	lock   sync.RWMutex
	data   map[string]cachedEntry
	expire time.Duration
	lru    lru // emptyLru no-op implementation when capacity is unlimited
	// noLimit means capacity is unlimited (WithLimit unset or <= 0).
	// When true the read path does not update the LRU, so it can take the read
	// lock, which improves concurrent read performance.
	noLimit  bool
	barrier  *syncx.SingleFlight[any]
	unstable *unstable
	name     string
	stats    *cacheStat
	stop     chan struct{}
	once     sync.Once
	// scanInterval is the global expiry scan interval, derived from the default
	// expiry given at construction time; it is zero - and no scan is started -
	// when no expiry is configured (expire <= 0).
	scanInterval time.Duration
	// ctx is the context held by the cache instance, used to correlate trace
	// information in background logs.
	ctx context.Context
}

// MemCacheOption customizes the behaviour of the in-memory cache.
type MemCacheOption func(*memCacheOptions)

// memCacheOptions holds the internal options of the in-memory cache.
// limit is the LRU capacity cap, 0 means unlimited; name is the cache name used to
// identify the cache in statistics logs.
type memCacheOptions struct {
	limit int
	name  string
}

// WithLimit sets the capacity cap of the cache; beyond it the least recently used
// key is evicted. limit <= 0 means unlimited capacity (the default).
func WithLimit(limit int) MemCacheOption {
	return func(o *memCacheOptions) { o.limit = limit }
}

// WithName sets the cache name, used to identify the cache in statistics logs.
func WithName(name string) MemCacheOption {
	return func(o *memCacheOptions) { o.name = name }
}

// NewMemCache creates and returns an in-memory cache instance.
// ctx is held by the cache instance and used to correlate trace information in the
// background statistics logs; expire is the default expiry; the options customize
// the capacity cap, the name, and so on.
//
//	mc := cache.NewMemCache(ctx, time.Minute, cache.WithLimit(1000))
func NewMemCache(ctx context.Context, expire time.Duration, opts ...MemCacheOption) *MemCache {
	var o memCacheOptions
	for _, opt := range opts {
		opt(&o)
	}

	c := &MemCache{
		data:     make(map[string]cachedEntry),
		expire:   expire,
		noLimit:  o.limit <= 0,
		barrier:  syncx.NewSingleFlight[any](),
		unstable: newUnstable(expiryDeviation),
		name:     o.name,
		stop:     make(chan struct{}),
		ctx:      ctx,
	}
	if c.name == "" {
		c.name = defaultCacheName
	}
	if o.limit > 0 {
		c.lru = newKeyLru(o.limit, c.onEvict)
	} else {
		c.lru = emptyLru{}
	}
	// Global expiry scan: started only when an expiry is configured. It shares the
	// stop channel with cacheStat and exits together with it on Close.
	if expire > 0 {
		c.scanInterval = scanIntervalFor(expire)
		go c.scanLoop()
	}
	c.stats = newCacheStat(c.ctx, c.name, c.size, c.stop)
	return c
}

// Get returns the value of key; it returns ErrNotFound on a miss or when the entry
// has expired. ctx is ignored by this implementation.
func (c *MemCache) Get(ctx context.Context, key string) (any, error) {
	value, ok := c.doGet(key)
	if ok {
		c.stats.IncrementHit()
		return value, nil
	}
	c.stats.IncrementMiss()
	return nil, ErrNotFound
}

// Set stores value in the cache using the default expiry.
// ctx is ignored by this implementation.
func (c *MemCache) Set(ctx context.Context, key string, value any) error {
	c.SetEx(ctx, key, value, c.expire)
	return nil
}

// SetEx stores value in the cache with the given time to live ttl.
// ctx is ignored by this implementation.
func (c *MemCache) SetEx(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.expire
	}

	// No expiry: write directly, constrained only by the LRU capacity.
	if ttl <= 0 {
		c.lock.Lock()
		c.data[key] = cachedEntry{value: value}
		c.lru.add(key)
		c.lock.Unlock()
		return nil
	}

	// Jitter the effective expiry so that a large number of keys does not expire at
	// the same instant (avalanche protection).
	expiry := c.unstable.AroundDuration(ttl)

	c.lock.Lock()
	c.data[key] = cachedEntry{value: value, expireAt: time.Now().Add(expiry)}
	c.lru.add(key)
	c.lock.Unlock()
	return nil
}

// Delete removes one or more keys.
// ctx is ignored by this implementation.
func (c *MemCache) Delete(ctx context.Context, keys ...string) error {
	c.lock.Lock()
	for _, key := range keys {
		delete(c.data, key)
		c.lru.remove(key)
	}
	c.lock.Unlock()
	return nil
}

// Take returns the value of key; on a miss it calls fetch, writes the result to the
// cache and returns it, protecting against cache stampedes.
// ctx is ignored by this implementation.
func (c *MemCache) Take(ctx context.Context, key string, fetch func() (any, error)) (any, error) {
	if val, ok := c.doGet(key); ok {
		c.stats.IncrementHit()
		return val, nil
	}

	var fresh bool
	val, err := c.barrier.Do(key, func() (any, error) {
		// Double check: another concurrent call may have written it while waiting.
		if val, ok := c.doGet(key); ok {
			return val, nil
		}

		v, e := fetch()
		if e != nil {
			return nil, e
		}

		fresh = true
		_ = c.Set(ctx, key, v)
		return v, nil
	})
	if err != nil {
		return nil, err
	}

	if fresh {
		c.stats.IncrementMiss()
	} else {
		// Hit the value written by another concurrent call.
		c.stats.IncrementHit()
	}
	return val, nil
}

// Increment adds delta to the numeric value of key; a missing key is initialized to
// delta. ctx is ignored by this implementation.
func (c *MemCache) Increment(ctx context.Context, key string, delta int64) error {
	return c.addDelta(key, delta)
}

// Decrement subtracts delta from the numeric value of key; a missing key is
// initialized to -delta. ctx is ignored by this implementation.
func (c *MemCache) Decrement(ctx context.Context, key string, delta int64) error {
	return c.addDelta(key, -delta)
}

// addDelta adds delta to the numeric value of key while holding the lock.
// When key does not exist or has expired it is initialized to delta with the default
// expiry.
func (c *MemCache) addDelta(key string, delta int64) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	entry, ok := c.data[key]
	if !ok || isExpired(entry) {
		c.data[key] = cachedEntry{value: delta, expireAt: c.newExpireAt()}
		c.lru.add(key)
		return nil
	}

	value, err := addToValue(entry.value, delta)
	if err != nil {
		return err
	}
	entry.value = value
	c.data[key] = entry
	c.lru.add(key)
	return nil
}

// newExpireAt returns the expiry instant based on the default expiry; it returns the
// zero value (never expires) when no expiry is configured.
func (c *MemCache) newExpireAt() time.Time {
	if c.expire <= 0 {
		return time.Time{}
	}
	return time.Now().Add(c.unstable.AroundDuration(c.expire))
}

// addToValue adds delta to v and returns a result of the same type; it returns an
// error when v is not numeric.
func addToValue(v any, delta int64) (any, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := rv.Int() + delta
		return reflect.ValueOf(n).Convert(rv.Type()).Interface(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n := int64(rv.Uint()) + delta
		if n < 0 {
			return nil, fmt.Errorf("cache: decrement underflow for %T", v)
		}
		return reflect.ValueOf(uint64(n)).Convert(rv.Type()).Interface(), nil
	case reflect.Float32, reflect.Float64:
		n := rv.Float() + float64(delta)
		return reflect.ValueOf(n).Convert(rv.Type()).Interface(), nil
	default:
		return nil, fmt.Errorf("cache: value of type %T is not numeric", v)
	}
}

// Expire sets the time to live of key; the key expires automatically afterwards.
// ttl <= 0 expires it immediately (the key is deleted); a missing key returns
// ErrNotFound. ctx is ignored by this implementation.
func (c *MemCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	entry, ok := c.data[key]
	if !ok {
		return ErrNotFound
	}
	if ttl <= 0 {
		delete(c.data, key)
		c.lru.remove(key)
		return nil
	}

	// Jitter the expiry instant so that a large number of keys does not expire at
	// the same instant (avalanche protection).
	entry.expireAt = time.Now().Add(c.unstable.AroundDuration(ttl))
	c.data[key] = entry
	return nil
}

// Size returns the current number of elements in the cache.
func (c *MemCache) Size() int { return c.size() }

// Close stops the background statistics goroutine.
func (c *MemCache) Close() {
	c.once.Do(func() { close(c.stop) })
}

// doGet reads the entry and updates the LRU access order while holding the lock.
// Lazy expiry: on a hit an expired entry is deleted and treated as a miss.
// Without a capacity limit (noLimit) the LRU is not updated, so the read lock
// suffices and multiple reads can run in parallel; when an expired entry is hit the
// lock is upgraded to the write lock before deleting (double-checked to avoid
// concurrent duplicate deletions). With LRU enabled a read hit updates the access
// order and must hold the write lock.
func (c *MemCache) doGet(key string) (any, bool) {
	if c.noLimit {
		c.lock.RLock()
		entry, ok := c.data[key]
		if !ok || !isExpired(entry) {
			c.lock.RUnlock()
			return entry.value, ok
		}
		c.lock.RUnlock()

		// Expired: upgrade to the write lock and delete after a double check.
		c.lock.Lock()
		entry, ok = c.data[key]
		if ok && isExpired(entry) {
			delete(c.data, key)
			ok = false
		}
		c.lock.Unlock()
		return entry.value, ok
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	entry, ok := c.data[key]
	if ok && isExpired(entry) {
		delete(c.data, key)
		c.lru.remove(key)
		ok = false
	} else if ok {
		c.lru.add(key)
	}
	return entry.value, ok
}

// isExpired reports whether the entry has expired; a zero expireAt never expires.
func isExpired(e cachedEntry) bool {
	return !e.expireAt.IsZero() && time.Now().After(e.expireAt)
}

// scanExpired is the global scan that removes every expired entry.
// The caller must hold c.lock (the scan runs under the write lock).
func (c *MemCache) scanExpired() {
	now := time.Now()
	c.lock.Lock()
	defer c.lock.Unlock()
	for k, e := range c.data {
		if !e.expireAt.IsZero() && now.After(e.expireAt) {
			delete(c.data, k)
			c.lru.remove(k)
		}
	}
}

// scanLoop runs the global expiry scan periodically; it shares the stop channel with
// cacheStat. It also watches the ctx passed at construction time: when the caller
// never calls Close (or forgets to), cancelling ctx reclaims this goroutine as well,
// preventing a background goroutine leak.
func (c *MemCache) scanLoop() {
	ticker := time.NewTicker(c.scanInterval)
	defer ticker.Stop()

	ctxDone := c.ctxDone()
	for {
		select {
		case <-ticker.C:
			c.scanExpired()
		case <-c.stop:
			return
		case <-ctxDone:
			return
		}
	}
}

// ctxDone returns the Done channel of the ctx passed at construction time; it returns
// nil when ctx is nil (blocks forever, i.e. nothing is watched).
func (c *MemCache) ctxDone() <-chan struct{} {
	if c.ctx == nil {
		return nil
	}
	return c.ctx.Done()
}

// scanIntervalFor derives the global scan interval from the default expiry.
// The interval is expire/4 clamped to [1ms, 1min], which avoids scanning an empty map
// too often while keeping expired keys from lingering for too long.
func scanIntervalFor(expire time.Duration) time.Duration {
	iv := expire / 4
	if iv < time.Millisecond {
		iv = time.Millisecond
	}
	if iv > time.Minute {
		iv = time.Minute
	}
	return iv
}

// onEvict is the LRU eviction callback. Note: it is called with c.lock held.
func (c *MemCache) onEvict(key string) {
	delete(c.data, key)
}

// size returns the current number of elements (it locks internally and is used as a
// statistics callback).
func (c *MemCache) size() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.data)
}

// --- LRU eviction (only the access order of keys is tracked; values live in data) ---

// lru defines the LRU eviction interface; it only needs to track the access order of keys.
type lru interface {
	add(key string)
	remove(key string)
}

// emptyLru is a no-op implementation; nothing is evicted when capacity is unlimited.
type emptyLru struct{}

func (emptyLru) add(string)    {}
func (emptyLru) remove(string) {}

// keyLru implements an O(1) LRU with a hash map plus a doubly linked list.
type keyLru struct {
	limit    int
	evicts   *list.List // front is most recently used, back is least recently used
	elements map[string]*list.Element
	onEvict  func(key string)
}

func newKeyLru(limit int, onEvict func(key string)) *keyLru {
	return &keyLru{
		limit:    limit,
		evicts:   list.New(),
		elements: make(map[string]*list.Element),
		onEvict:  onEvict,
	}
}

// add records one access: an existing key is moved to the front, otherwise it is
// inserted at the front and the capacity is checked.
func (klru *keyLru) add(key string) {
	if elem, ok := klru.elements[key]; ok {
		klru.evicts.MoveToFront(elem)
		return
	}

	elem := klru.evicts.PushFront(key)
	klru.elements[key] = elem

	if klru.evicts.Len() > klru.limit {
		klru.removeOldest()
	}
}

// remove drops the access record of the given key.
func (klru *keyLru) remove(key string) {
	if elem, ok := klru.elements[key]; ok {
		klru.removeElement(elem)
	}
}

// removeOldest evicts the least recently used key.
func (klru *keyLru) removeOldest() {
	elem := klru.evicts.Back()
	if elem != nil {
		klru.removeElement(elem)
	}
}

// removeElement removes the node from the list and triggers the eviction callback.
func (klru *keyLru) removeElement(e *list.Element) {
	klru.evicts.Remove(e)
	key := e.Value.(string)
	delete(klru.elements, key)
	klru.onEvict(key)
}

// --- Expiry jitter ---

// unstable generates random values around a base value.
// It uses the global functions of math/rand/v2 (concurrency-safe and lock-free), so
// no lock of its own is needed.
type unstable struct {
	deviation float64
}

func newUnstable(deviation float64) *unstable {
	if deviation < 0 {
		deviation = 0
	}
	if deviation > 1 {
		deviation = 1
	}
	return &unstable{deviation: deviation}
}

// AroundDuration returns base with jitter applied, within [(1-d)*base, (1+d)*base].
func (u *unstable) AroundDuration(base time.Duration) time.Duration {
	return time.Duration((1 + u.deviation - 2*u.deviation*rand.Float64()) * float64(base))
}

// --- Hit-ratio statistics ---

// cacheStat collects cache hit statistics and logs them periodically.
type cacheStat struct {
	name         string
	hit          uint64
	miss         uint64
	sizeCallback func() int
	interval     time.Duration
	stop         <-chan struct{}
	ctx          context.Context
}

func newCacheStat(ctx context.Context, name string, sizeCallback func() int, stop <-chan struct{}) *cacheStat {
	return newCacheStatWithInterval(ctx, statInterval, name, sizeCallback, stop)
}

// newCacheStatWithInterval creates a cacheStat with the given statistics interval; it
// exists to make testing easier.
func newCacheStatWithInterval(ctx context.Context, interval time.Duration, name string, sizeCallback func() int, stop <-chan struct{}) *cacheStat {
	st := &cacheStat{
		name:         name,
		sizeCallback: sizeCallback,
		interval:     interval,
		stop:         stop,
		ctx:          ctx,
	}
	go st.statLoop()
	return st
}

func (cs *cacheStat) IncrementHit() {
	atomic.AddUint64(&cs.hit, 1)
}

func (cs *cacheStat) IncrementMiss() {
	atomic.AddUint64(&cs.miss, 1)
}

func (cs *cacheStat) statLoop() {
	ticker := time.NewTicker(cs.interval)
	defer ticker.Stop()

	// Also watch ctx: when the caller does not call Close, cancelling ctx reclaims
	// this goroutine as well.
	var ctxDone <-chan struct{}
	if cs.ctx != nil {
		ctxDone = cs.ctx.Done()
	}

	for {
		select {
		case <-ticker.C:
			hit := atomic.SwapUint64(&cs.hit, 0)
			miss := atomic.SwapUint64(&cs.miss, 0)
			total := hit + miss
			if total == 0 {
				continue
			}
			percent := 100 * float32(hit) / float32(total)
			logger.InfofCtx(cs.ctx, "cache(%s) - qpm: %d, hit_ratio: %.1f%%, elements: %d, hit: %d, miss: %d",
				cs.name, total, percent, cs.sizeCallback(), hit, miss)
		case <-cs.stop:
			return
		case <-ctxDone:
			return
		}
	}
}
