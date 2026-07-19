package syncx

import (
	"context"
	"sync"
)

// OrDone 返回一个 channel，当 ctx.Done() 或 src 关闭时关闭。
// 用于在 select 中简化 context 取消的处理。
//
// 用法：
//
//	for v := range syncx.OrDone(ctx.Done(), src) {
//	    // 处理 v
//	}
func OrDone[T any](done <-chan struct{}, src <-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case v, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-done:
				}
			}
		}
	}()
	return out
}

// OrDoneCtx 返回一个 channel，当 ctx 取消或 src 关闭时关闭。
// 与 OrDone 类似，但直接接收 context.Context。
func OrDoneCtx[T any](ctx context.Context, src <-chan T) <-chan T {
	return OrDone[T](ctx.Done(), src)
}

// Merge 将多个 channel 合并为一个 channel，当所有源 channel 都关闭时关闭。
// 支持通过 context 取消。
func Merge[T any](ctx context.Context, channels ...<-chan T) <-chan T {
	out := make(chan T)
	var wg sync.WaitGroup
	wg.Add(len(channels))

	for _, ch := range channels {
		go func(c <-chan T) {
			defer wg.Done()
			for v := range OrDoneCtx(ctx, c) {
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// FanOut 将输入 channel 的值分发到 n 个输出 channel。
// 用于并行处理。
func FanOut[T any](ctx context.Context, src <-chan T, n int) []<-chan T {
	outs := make([]chan T, n)
	for i := range outs {
		outs[i] = make(chan T)
	}

	go func() {
		defer func() {
			for _, out := range outs {
				close(out)
			}
		}()

		for v := range OrDoneCtx(ctx, src) {
			for _, out := range outs {
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	result := make([]<-chan T, n)
	for i, out := range outs {
		result[i] = out
	}
	return result
}
