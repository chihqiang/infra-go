package syncx

import (
	"context"
	"fmt"
	"sync"
)

// --- SingleFlight ---

// singleFlightCall 表示一次正在执行的调用。
type singleFlightCall[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
}

// SingleFlight 防止缓存击穿，相同 key 的并发调用只执行一次，结果共享给所有调用者。
//
// 适用场景：
//   - 缓存击穿防护：大量并发请求同时查询同一个 key，只穿透到底层数据源一次
//   - 重复请求合并：多个协程请求相同资源，只执行一次实际获取操作
//
// 用法：
//
//	sf := syncx.NewSingleFlight[string]()
//	val, err := sf.Do("user:123", func() (string, error) {
//	    return fetchUserFromDB(123)
//	})
type SingleFlight[T any] struct {
	mu    sync.Mutex
	calls map[string]*singleFlightCall[T]
}

// NewSingleFlight 创建一个新的 SingleFlight 实例。
func NewSingleFlight[T any]() *SingleFlight[T] {
	return &SingleFlight[T]{
		calls: make(map[string]*singleFlightCall[T]),
	}
}

// Do 执行函数 fn，相同 key 的并发调用只执行一次。
// 如果已有相同 key 的调用正在执行，当前调用会等待其结果。
func (sf *SingleFlight[T]) Do(key string, fn func() (T, error)) (T, error) {
	sf.mu.Lock()
	if call, ok := sf.calls[key]; ok {
		sf.mu.Unlock()
		call.wg.Wait()
		return call.val, call.err
	}

	call := &singleFlightCall[T]{}
	call.wg.Add(1)
	sf.calls[key] = call
	sf.mu.Unlock()

	call.val, call.err = fn()
	call.wg.Done()

	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()

	return call.val, call.err
}

// DoCtx 执行函数 fn，支持 context 取消。
// 如果 context 取消，等待中的调用会返回 context 错误，但正在执行的调用不会中断。
// 注意：context 取消后，内部用于等待结果的 goroutine 会存活到 fn 执行完成，
// fn 完成后自动退出，不会造成持久性泄漏。
func (sf *SingleFlight[T]) DoCtx(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	// 快速检查 context
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}

	sf.mu.Lock()
	if call, ok := sf.calls[key]; ok {
		sf.mu.Unlock()

		// 等待结果或 context 取消
		done := make(chan struct{})
		go func() {
			call.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			return call.val, call.err
		case <-ctx.Done():
			var zero T
			return zero, fmt.Errorf("singleflight: context cancelled: %w", ctx.Err())
		}
	}

	call := &singleFlightCall[T]{}
	call.wg.Add(1)
	sf.calls[key] = call
	sf.mu.Unlock()

	call.val, call.err = fn(ctx)
	call.wg.Done()

	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()

	return call.val, call.err
}

// Forget 移除指定 key 的调用记录，使下一次 Do 调用会重新执行 fn。
// 不会影响正在执行的调用。
func (sf *SingleFlight[T]) Forget(key string) {
	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()
}
