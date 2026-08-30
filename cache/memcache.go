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

// 统计与过期参数。
const (
	defaultCacheName = "cache"
	statInterval     = time.Minute
	// 过期时间抖动幅度，将实际过期时间波动在 [0.95, 1.05] * expire，
	// 避免大量 key 在同一时刻过期（缓存雪崩防护）。
	expiryDeviation = 0.05
)

// cachedEntry 缓存条目。
// expireAt 为过期时刻，零值表示永不过期（构造时 expire<=0 的场景）。
// 过期判断基于 expireAt：Get 命中时惰性删除，另由全局扫描兜底清理。
// 无需版本号：没有定时器回调，最后一次写入的 expireAt 即当前生效值。
type cachedEntry struct {
	value    any
	expireAt time.Time
}

// MemCache 是 Cache 接口的内存实现。
//
// 特性：
//   - 过期删除：惰性删除（Get 命中时检查）+ 全局定期扫描，避免每 key 一个定时器
//   - LRU 淘汰：容量受限时淘汰最久未使用的 key
//   - 防击穿：Take 通过 SingleFlight 合并相同 key 的并发请求
//   - 命中率统计：周期性输出 QPS、命中率、元素数量
type MemCache struct {
	// lock 保护 data 与 lru。使用 RWMutex：无容量限制（noLimit）时
	// 读路径只需读锁即可并行；启用 LRU 时读命中会更新访问顺序，需写锁。
	lock   sync.RWMutex
	data   map[string]cachedEntry
	expire time.Duration
	lru    lru // 容量不限制时为 emptyLru 空实现
	// noLimit 表示不限制容量（WithLimit 未设置或 <= 0）。
	// 为 true 时读路径不更新 LRU，可走读锁，提高并发读性能。
	noLimit  bool
	barrier  *syncx.SingleFlight[any]
	unstable *unstable
	name     string
	stats    *cacheStat
	stop     chan struct{}
	once     sync.Once
	// scanInterval 全局过期扫描间隔，基于构造时的默认过期时间推导；
	// 未配置过期时间（expire<=0）时为零，不启动扫描。
	scanInterval time.Duration
	// ctx 缓存实例持有的上下文，用于后台日志关联链路信息。
	ctx context.Context
}

// MemCacheOption 用于自定义内存缓存行为。
type MemCacheOption func(*memCacheOptions)

// memCacheOptions 内存缓存内部选项。
// limit 为 LRU 容量上限，0 表示不限制；name 为缓存名称，用于统计日志标识。
type memCacheOptions struct {
	limit int
	name  string
}

// WithLimit 设置缓存容量上限，超出后按 LRU 淘汰最久未使用的 key。
// limit <= 0 表示不限制容量（默认）。
func WithLimit(limit int) MemCacheOption {
	return func(o *memCacheOptions) { o.limit = limit }
}

// WithName 设置缓存名称，用于统计日志标识。
func WithName(name string) MemCacheOption {
	return func(o *memCacheOptions) { o.name = name }
}

// NewMemCache 创建并返回一个内存缓存实例。
// ctx 由缓存实例持有，用于后台统计日志关联链路信息；
// expire 为默认过期时间；通过选项可定制容量上限、名称等。
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
	// 全局过期扫描：仅在配置了过期时间时启动，
	// 与 cacheStat 共用 stop channel，Close 时一并退出。
	if expire > 0 {
		c.scanInterval = scanIntervalFor(expire)
		go c.scanLoop()
	}
	c.stats = newCacheStat(c.ctx, c.name, c.size, c.stop)
	return c
}

// Get 返回指定 key 的值；未命中或已过期返回 ErrNotFound。
// ctx 在本实现中被忽略。
func (c *MemCache) Get(ctx context.Context, key string) (any, error) {
	value, ok := c.doGet(key)
	if ok {
		c.stats.IncrementHit()
		return value, nil
	}
	c.stats.IncrementMiss()
	return nil, ErrNotFound
}

// Set 将 value 存入缓存，使用默认过期时间。
// ctx 在本实现中被忽略。
func (c *MemCache) Set(ctx context.Context, key string, value any) error {
	c.SetEx(ctx, key, value, c.expire)
	return nil
}

// SetEx 将 value 存入缓存并指定存活时间 ttl。
// ctx 在本实现中被忽略。
func (c *MemCache) SetEx(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.expire
	}

	// 无过期时间：直接写入，仅受 LRU 容量约束。
	if ttl <= 0 {
		c.lock.Lock()
		c.data[key] = cachedEntry{value: value}
		c.lru.add(key)
		c.lock.Unlock()
		return nil
	}

	// 实际过期时间加抖动，避免大量 key 同时过期（雪崩防护）。
	expiry := c.unstable.AroundDuration(ttl)

	c.lock.Lock()
	c.data[key] = cachedEntry{value: value, expireAt: time.Now().Add(expiry)}
	c.lru.add(key)
	c.lock.Unlock()
	return nil
}

// Delete 删除一个或多个 key。
// ctx 在本实现中被忽略。
func (c *MemCache) Delete(ctx context.Context, keys ...string) error {
	c.lock.Lock()
	for _, key := range keys {
		delete(c.data, key)
		c.lru.remove(key)
	}
	c.lock.Unlock()
	return nil
}

// Take 返回 key 的值；未命中时调用 fetch 获取并写入缓存，防缓存击穿。
// ctx 在本实现中被忽略。
func (c *MemCache) Take(ctx context.Context, key string, fetch func() (any, error)) (any, error) {
	if val, ok := c.doGet(key); ok {
		c.stats.IncrementHit()
		return val, nil
	}

	var fresh bool
	val, err := c.barrier.Do(key, func() (any, error) {
		// 二次检查：等待期间可能已被其他并发调用写入。
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
		// 命中其他并发调用写入的结果。
		c.stats.IncrementHit()
	}
	return val, nil
}

// Increment 将 key 对应的数值自增 delta；key 不存在时初始化为 delta。
// ctx 在本实现中被忽略。
func (c *MemCache) Increment(ctx context.Context, key string, delta int64) error {
	return c.addDelta(key, delta)
}

// Decrement 将 key 对应的数值自减 delta；key 不存在时初始化为 -delta。
// ctx 在本实现中被忽略。
func (c *MemCache) Decrement(ctx context.Context, key string, delta int64) error {
	return c.addDelta(key, -delta)
}

// addDelta 在锁保护下对 key 对应的数值加上 delta。
// key 不存在或已过期时，以默认过期时间初始化为 delta。
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

// newExpireAt 返回基于默认过期时间的过期时刻；未配置过期时间返回零值（永不过期）。
func (c *MemCache) newExpireAt() time.Time {
	if c.expire <= 0 {
		return time.Time{}
	}
	return time.Now().Add(c.unstable.AroundDuration(c.expire))
}

// addToValue 将 delta 加到 v 上并返回同类型结果；v 非数值类型时返回错误。
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

// Expire 为 key 设置存活时间 ttl，到期后自动失效。
// ttl <= 0 时立即失效（删除该 key）；key 不存在返回 ErrNotFound。
// ctx 在本实现中被忽略。
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

	// 过期时刻加抖动，避免大量 key 同时过期（雪崩防护）。
	entry.expireAt = time.Now().Add(c.unstable.AroundDuration(ttl))
	c.data[key] = entry
	return nil
}

// Size 返回当前缓存元素数量。
func (c *MemCache) Size() int { return c.size() }

// Close 停止后台统计 goroutine。
func (c *MemCache) Close() {
	c.once.Do(func() { close(c.stop) })
}

// doGet 在锁保护下读取并更新 LRU 访问顺序。
// 惰性过期：命中时若已过期则删除并视为未命中。
// 无容量限制（noLimit）时不更新 LRU，使用读锁即可，多个读可并行；
// 命中已过期条目时升级为写锁再删除（double-check 防并发重复删除）。
// 启用 LRU 时读命中会更新访问顺序，需持写锁。
func (c *MemCache) doGet(key string) (any, bool) {
	if c.noLimit {
		c.lock.RLock()
		entry, ok := c.data[key]
		if !ok || !isExpired(entry) {
			c.lock.RUnlock()
			return entry.value, ok
		}
		c.lock.RUnlock()

		// 已过期：升级写锁，double-check 后删除。
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

// isExpired 判断条目是否已过期；expireAt 为零值表示永不过期。
func isExpired(e cachedEntry) bool {
	return !e.expireAt.IsZero() && time.Now().After(e.expireAt)
}

// scanExpired 全局扫描，清理所有已过期条目。
// 调用时须持有 c.lock（扫描在写锁下进行）。
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

// scanLoop 周期性地执行全局过期扫描，与 cacheStat 共用 stop channel。
func (c *MemCache) scanLoop() {
	ticker := time.NewTicker(c.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.scanExpired()
		case <-c.stop:
			return
		}
	}
}

// scanIntervalFor 根据默认过期时间推导全局扫描间隔。
// 间隔取 expire/4，并限制在 [1ms, 1min]，
// 避免过频扫描空 map 或间隔过长导致过期 key 长期滞留。
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

// onEvict LRU 淘汰回调。注意：调用时已持有 c.lock。
func (c *MemCache) onEvict(key string) {
	delete(c.data, key)
}

// size 返回当前元素数量（内部加锁，供统计回调使用）。
func (c *MemCache) size() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.data)
}

// --- LRU 淘汰（仅维护 key 的访问顺序，值存于 data map） ---

// lru 定义 LRU 淘汰接口，仅需记录 key 的访问顺序。
type lru interface {
	add(key string)
	remove(key string)
}

// emptyLru 空实现，容量不限制时不进行任何淘汰。
type emptyLru struct{}

func (emptyLru) add(string)    {}
func (emptyLru) remove(string) {}

// keyLru 使用哈希表 + 双向链表实现 O(1) 的 LRU。
type keyLru struct {
	limit    int
	evicts   *list.List // 链表头为最近使用，尾部为最久未使用
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

// add 记录一次访问：已存在则移到头部，否则插入头部并检查容量。
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

// remove 删除指定 key 的访问记录。
func (klru *keyLru) remove(key string) {
	if elem, ok := klru.elements[key]; ok {
		klru.removeElement(elem)
	}
}

// removeOldest 淘汰最久未使用的 key。
func (klru *keyLru) removeOldest() {
	elem := klru.evicts.Back()
	if elem != nil {
		klru.removeElement(elem)
	}
}

// removeElement 从链表中移除节点并触发淘汰回调。
func (klru *keyLru) removeElement(e *list.Element) {
	klru.evicts.Remove(e)
	key := e.Value.(string)
	delete(klru.elements, key)
	klru.onEvict(key)
}

// --- 过期时间抖动 ---

// unstable 生成围绕基准值的随机值。
// 使用 math/rand/v2 的全局函数（并发安全且无锁），无需自建锁保护。
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

// AroundDuration 返回围绕 base 抖动后的时长，范围为 [(1-d)*base, (1+d)*base]。
func (u *unstable) AroundDuration(base time.Duration) time.Duration {
	return time.Duration((1 + u.deviation - 2*u.deviation*rand.Float64()) * float64(base))
}

// --- 命中率统计 ---

// cacheStat 缓存命中统计，周期性输出日志。
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

// newCacheStatWithInterval 以指定统计周期创建 cacheStat，便于测试。
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
		}
	}
}
