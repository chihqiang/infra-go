package syncx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- OrDone 测试 ---

func TestOrDone(t *testing.T) {
	src := make(chan int, 5)
	done := make(chan struct{})

	for i := 1; i <= 5; i++ {
		src <- i
	}
	close(src)

	var result []int
	for v := range OrDone(done, src) {
		result = append(result, v)
	}

	assert.Equal(t, []int{1, 2, 3, 4, 5}, result)
}

func TestOrDone_Cancelled(t *testing.T) {
	src := make(chan int)
	done := make(chan struct{})

	go func() {
		src <- 1
		time.Sleep(10 * time.Millisecond)
		close(done) // 取消
	}()

	var result []int
	for v := range OrDone(done, src) {
		result = append(result, v)
	}

	// 只收到第一个值就因 done 关闭而退出
	assert.Equal(t, []int{1}, result)
}

func TestOrDoneCtx(t *testing.T) {
	src := make(chan string, 3)
	ctx, cancel := context.WithCancel(context.Background())

	src <- "a"
	src <- "b"
	src <- "c"
	close(src)

	var result []string
	for v := range OrDoneCtx(ctx, src) {
		result = append(result, v)
	}
	cancel()

	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestOrDoneCtx_Cancelled(t *testing.T) {
	src := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		src <- 1
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	var result []int
	for v := range OrDoneCtx(ctx, src) {
		result = append(result, v)
	}

	assert.Equal(t, []int{1}, result)
}

// --- Merge 测试 ---

func TestMerge(t *testing.T) {
	ch1 := make(chan int, 3)
	ch2 := make(chan int, 3)
	ch3 := make(chan int, 3)

	ch1 <- 1
	ch1 <- 2
	ch1 <- 3
	close(ch1)

	ch2 <- 10
	ch2 <- 20
	close(ch2)

	ch3 <- 100
	close(ch3)

	ctx := context.Background()
	var result []int
	for v := range Merge(ctx, ch1, ch2, ch3) {
		result = append(result, v)
	}

	assert.Len(t, result, 6)
	assert.Contains(t, result, 1)
	assert.Contains(t, result, 100)
}

func TestMerge_NoChannels(t *testing.T) {
	// 无输入 channel 时立即关闭
	out := Merge[int](context.Background())
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan int) // 永不关闭的源

	go func() {
		ch <- 1
	}()

	out := Merge(ctx, ch)

	val, ok := <-out
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	// ctx 取消后，merge 应尽快关闭，即使源 channel 仍开着
	cancel()
	select {
	case _, ok := <-out:
		assert.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("Merge did not close after context cancel")
	}
}

func TestMerge_ManyChannels(t *testing.T) {
	const n = 20
	channels := make([]<-chan int, 0, n)
	total := 0
	for i := 0; i < n; i++ {
		ch := make(chan int, 1)
		ch <- i
		close(ch)
		channels = append(channels, ch)
		total++
	}

	count := 0
	for range Merge(context.Background(), channels...) {
		count++
	}
	assert.Equal(t, total, count)
}

// --- FanOut 测试 ---

func TestFanOut(t *testing.T) {
	src := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		src <- i
	}
	close(src)

	ctx := context.Background()
	outs := FanOut(ctx, src, 3)

	var allValues []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, out := range outs {
		wg.Add(1)
		go func(ch <-chan int) {
			defer wg.Done()
			for v := range ch {
				mu.Lock()
				allValues = append(allValues, v)
				mu.Unlock()
			}
		}(out)
	}
	wg.Wait()

	// 每个 output channel 都应该收到全部 6 个值
	assert.Len(t, allValues, 18) // 3 outputs × 6 values
}

func TestFanOut_Single(t *testing.T) {
	src := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		src <- i
	}
	close(src)

	outs := FanOut(context.Background(), src, 1)
	var got []int
	for v := range outs[0] {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3}, got)
}

func TestFanOut_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := make(chan int)

	outs := FanOut(ctx, src, 2)

	// 发一个值，确认广播正常
	src <- 42
	for _, out := range outs {
		select {
		case v := <-out:
			assert.Equal(t, 42, v)
		case <-time.After(2 * time.Second):
			t.Fatal("FanOut did not deliver value")
		}
	}

	// ctx 取消后应解除阻塞并关闭所有输出
	cancel()
	for _, out := range outs {
		select {
		case _, ok := <-out:
			if ok {
				t.Fatal("expected output channel to be closed")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("FanOut output not closed after context cancel")
		}
	}
}
