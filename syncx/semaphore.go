package syncx

import "sync"

// Semaphore 信号量，用于控制并发数量。
//
// 适用场景：
//   - 限制并发请求数
//   - 限制资源池使用
//   - 批量任务并发控制
//
// 用法：
//
//	sem := syncx.NewSemaphore(10) // 最多 10 个并发
//	for _, task := range tasks {
//	    sem.Acquire()
//	    go func() {
//	        defer sem.Release()
//	        doWork(task)
//	    }()
//	}
//	sem.Wait() // 等待所有完成
type Semaphore struct {
	pool chan struct{}
	wg   sync.WaitGroup
}

// NewSemaphore 创建一个指定并发数的信号量。
func NewSemaphore(max int) *Semaphore {
	if max <= 0 {
		max = 1
	}
	return &Semaphore{
		pool: make(chan struct{}, max),
	}
}

// Acquire 获取一个信号量，如果已满则阻塞等待。
func (s *Semaphore) Acquire() {
	s.wg.Add(1)
	s.pool <- struct{}{}
}

// TryAcquire 尝试获取一个信号量，如果已满则返回 false。
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

// Release 释放一个信号量。
func (s *Semaphore) Release() {
	<-s.pool
	s.wg.Done()
}

// Wait 等待所有已获取的信号量被释放。
func (s *Semaphore) Wait() {
	s.wg.Wait()
}

// Capacity 返回信号量的最大并发数。
func (s *Semaphore) Capacity() int {
	return cap(s.pool)
}

// Available 返回当前可用的信号量数量。
func (s *Semaphore) Available() int {
	return cap(s.pool) - len(s.pool)
}
