package breaker

import (
	"sync"
	"time"
)

// Statistical event types.
const (
	success = iota
	fail
	drop
)

// bucket is a single time bucket of the rolling window, accumulating the request
// statistics for one interval.
type bucket struct {
	Sum     int64 // total requests
	Success int64 // successes
	Failure int64 // failures
	Drop    int64 // requests rejected by the breaker
}

// add accumulates the statistic according to the event type.
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

// reset clears the statistics of the bucket.
func (b *bucket) reset() {
	b.Sum = 0
	b.Success = 0
	b.Failure = 0
	b.Drop = 0
}

// rollingWindow is a time-based rolling window that splits the statistical period
// into several time buckets.
// Writes advance offset automatically and reset expired buckets; reads aggregate
// all non-expired buckets.
type rollingWindow struct {
	lock     sync.RWMutex
	size     int           // number of buckets
	interval time.Duration // time span of each bucket
	offset   int           // index of the bucket currently being written
	lastTime time.Time     // time of the last write
	buckets  []bucket
}

// newRollingWindow creates a rolling window whose total span is size * interval.
func newRollingWindow(size int, interval time.Duration) *rollingWindow {
	return &rollingWindow{
		size:     size,
		interval: interval,
		buckets:  make([]bucket, size),
		lastTime: time.Now(),
	}
}

// add puts the event into the current time bucket.
func (rw *rollingWindow) add(v int64) {
	rw.lock.Lock()
	defer rw.lock.Unlock()
	rw.updateOffset()
	rw.buckets[rw.offset].add(v)
}

// span returns how many buckets have elapsed since lastTime, capped at size.
func (rw *rollingWindow) span() int {
	offset := int(time.Since(rw.lastTime) / rw.interval)
	if offset >= 0 && offset < rw.size {
		return offset
	}
	return rw.size
}

// updateOffset advances offset and resets the expired buckets.
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
	// Align to the interval boundary to avoid accumulating drift
	rw.lastTime = now.Add(-(now.Sub(rw.lastTime) % rw.interval))
}
