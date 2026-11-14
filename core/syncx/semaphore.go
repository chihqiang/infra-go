package syncx

import "sync"

// Semaphore limits how many operations may run concurrently.
//
// Use cases:
//   - capping the number of in-flight requests
//   - limiting the use of a resource pool
//   - bounding the concurrency of batch tasks
//
// Usage:
//
//	sem := syncx.NewSemaphore(10) // at most 10 concurrent operations
//	for _, task := range tasks {
//	    sem.Acquire()
//	    go func() {
//	        defer sem.Release()
//	        doWork(task)
//	    }()
//	}
//	sem.Wait() // wait for all of them to finish
type Semaphore struct {
	pool chan struct{}
	wg   sync.WaitGroup
}

// NewSemaphore creates a semaphore with the given concurrency limit.
func NewSemaphore(max int) *Semaphore {
	if max <= 0 {
		max = 1
	}
	return &Semaphore{
		pool: make(chan struct{}, max),
	}
}

// Acquire takes one slot, blocking while the semaphore is full.
func (s *Semaphore) Acquire() {
	s.wg.Add(1)
	s.pool <- struct{}{}
}

// TryAcquire attempts to take one slot and reports false when the semaphore is full.
func (s *Semaphore) TryAcquire() bool {
	s.wg.Add(1)
	select {
	case s.pool <- struct{}{}:
		return true
	default:
		s.wg.Done()
		return false
	}
}

// Release frees one slot.
func (s *Semaphore) Release() {
	<-s.pool
	s.wg.Done()
}

// Wait blocks until every acquired slot has been released.
func (s *Semaphore) Wait() {
	s.wg.Wait()
}

// Capacity returns the maximum concurrency of the semaphore.
func (s *Semaphore) Capacity() int {
	return cap(s.pool)
}

// Available returns how many slots are currently free.
func (s *Semaphore) Available() int {
	return cap(s.pool) - len(s.pool)
}
