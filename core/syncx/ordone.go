package syncx

import (
	"context"
	"sync"
)

// OrDone returns a channel that is closed when done is closed or src is closed.
// It simplifies handling context cancellation inside a select.
//
// Usage:
//
//	for v := range syncx.OrDone(ctx.Done(), src) {
//	    // handle v
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

// OrDoneCtx returns a channel that is closed when ctx is cancelled or src is
// closed. It is like OrDone but takes a context.Context directly.
func OrDoneCtx[T any](ctx context.Context, src <-chan T) <-chan T {
	return OrDone[T](ctx.Done(), src)
}

// Merge fans several channels into one, closing the result once every source
// channel is closed. Cancellation is supported through the context.
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

// FanOut broadcasts every value from src to all n output channels.
// Each output channel receives every value read from src.
// A slow consumer on one output channel never blocks consumers of the other
// output channels. When the context is cancelled, every blocked send unblocks
// automatically.
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
			v := v
			var wg sync.WaitGroup
			// Broadcast concurrently to all outputs, so slow consumers block nobody
			for i := range outs {
				wg.Add(1)
				go func(out chan T) {
					defer wg.Done()
					select {
					case out <- v:
					case <-ctx.Done():
					}
				}(outs[i])
			}
			wg.Wait()
		}
	}()

	result := make([]<-chan T, n)
	for i, out := range outs {
		result[i] = out
	}
	return result
}
