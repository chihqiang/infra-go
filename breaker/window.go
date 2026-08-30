package breaker

import (
	"sync"
	"time"
)

// 统计事件类型。
const (
	success = iota
	fail
	drop
)

// bucket 滑动窗口中的单个时间桶，累计一段时间内的请求统计。
type bucket struct {
	Sum     int64 // 总请求数
	Success int64 // 成功数
	Failure int64 // 失败数
	Drop    int64 // 被熔断拒绝数
}

// add 按事件类型累加统计。
func (b *bucket) add(v int64) {
	switch v {
	case fail:
		b.Sum++
		b.Failure++
	case drop:
		b.Sum++
		b.Drop++
	default:
		b.Sum++
		b.Success++
	}
}

// reset 清空桶内统计。
func (b *bucket) reset() {
	b.Sum = 0
	b.Success = 0
	b.Failure = 0
	b.Drop = 0
}

// rollingWindow 时间滑动窗口，将统计周期划分为多个时间桶。
// 写入时自动推进 offset 并重置过期桶；读取时聚合所有未过期桶。
type rollingWindow struct {
	lock     sync.RWMutex
	size     int           // 桶数量
	interval time.Duration // 每个桶的时间跨度
	offset   int           // 当前写入桶下标
	lastTime time.Time     // 上一次写入的时间
	buckets  []bucket
}

// newRollingWindow 创建滑动窗口，总统计窗口为 size * interval。
func newRollingWindow(size int, interval time.Duration) *rollingWindow {
	return &rollingWindow{
		size:     size,
		interval: interval,
		buckets:  make([]bucket, size),
		lastTime: time.Now(),
	}
}

// add 将事件加入当前时间桶。
func (rw *rollingWindow) add(v int64) {
	rw.lock.Lock()
	defer rw.lock.Unlock()
	rw.updateOffset()
	rw.buckets[rw.offset].add(v)
}

// reduce 对所有有效时间桶执行 fn 聚合统计。
// span 为 0（刚写入过）时包含当前桶；span 越大，跳过的"部分数据"桶越多。
func (rw *rollingWindow) reduce(fn func(*bucket)) {
	rw.lock.RLock()
	defer rw.lock.RUnlock()

	span := rw.span()
	diff := rw.size - span
	if diff <= 0 {
		return
	}

	start := (rw.offset + span + 1) % rw.size
	for i := 0; i < diff; i++ {
		fn(&rw.buckets[(start+i)%rw.size])
	}
}

// span 计算自 lastTime 以来应跨越的桶数，超过窗口返回 size。
func (rw *rollingWindow) span() int {
	offset := int(time.Since(rw.lastTime) / rw.interval)
	if offset >= 0 && offset < rw.size {
		return offset
	}
	return rw.size
}

// updateOffset 推进 offset 并重置过期的桶。
func (rw *rollingWindow) updateOffset() {
	span := rw.span()
	if span <= 0 {
		return
	}

	offset := rw.offset
	for i := 0; i < span; i++ {
		rw.buckets[(offset+i+1)%rw.size].reset()
	}
	rw.offset = (offset + span) % rw.size
	now := time.Now()
	// 对齐到 interval 边界，避免累积误差
	rw.lastTime = now.Add(-(now.Sub(rw.lastTime) % rw.interval))
}
