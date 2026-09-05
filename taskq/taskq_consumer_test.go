package taskq

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件补充 Consumer 侧未覆盖 API 的测试：
//   - Handle（asynq.Handler 接口对象注册）
//   - Use（中间件链）
//   - Run（阻塞运行：错误分支 + 信号驱动退出）
//
// 复用 taskq_test.go 的 helper：newMiniRedis/testConfig。

// countingTaskHandler 实现 asynq.Handler 接口，供 Handle 测试使用。
type countingTaskHandler struct {
	count *int64
}

func (h *countingTaskHandler) ProcessTask(_ context.Context, _ *asynq.Task) error {
	atomic.AddInt64(h.count, 1)
	return nil
}

// TestConsumer_HandleAndUse 端到端验证 Handle（Handler 对象）与 Use（中间件）注册生效。
func TestConsumer_HandleAndUse(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	con := NewConsumer(cfg, nil)

	var (
		mwCalls int64
		handled int64
	)
	var lastTask atomic.Value

	// Use：中间件在 handler 之前执行，可校验/改写任务
	con.Use(func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			atomic.AddInt64(&mwCalls, 1)
			lastTask.Store(task.Type())
			return next.ProcessTask(ctx, task)
		})
	})

	con.Handle("test:handle", &countingTaskHandler{count: &handled})

	require.NoError(t, con.Start())
	defer con.Shutdown()

	producer := NewProducer(cfg)
	defer producer.Close()

	for i := 0; i < 3; i++ {
		_, err := producer.Enqueue(context.Background(), asynq.NewTask("test:handle", nil))
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&handled) == 3
	}, 5*time.Second, 100*time.Millisecond)

	// 中间件执行次数与任务数一致，且能看到任务类型
	assert.Equal(t, int64(3), atomic.LoadInt64(&mwCalls))
	assert.Equal(t, "test:handle", lastTask.Load().(string))
}

// TestConsumer_HandleWithDifferentPatterns 验证 Handle 注册不同前缀 pattern，
// 各自精确路由到对应 handler（asynq mux 为最长前缀匹配）。
func TestConsumer_HandleWithDifferentPatterns(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	con := NewConsumer(cfg, nil)

	var handledA, handledB int64
	con.Handle("diff:a", &countingTaskHandler{count: &handledA})
	con.Handle("diff:b", &countingTaskHandler{count: &handledB})

	require.NoError(t, con.Start())
	defer con.Shutdown()

	producer := NewProducer(cfg)
	defer producer.Close()

	_, err := producer.Enqueue(context.Background(), asynq.NewTask("diff:a:alpha", nil))
	require.NoError(t, err)
	_, err = producer.Enqueue(context.Background(), asynq.NewTask("diff:b:beta", nil))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&handledA) == 1 && atomic.LoadInt64(&handledB) == 1
	}, 5*time.Second, 100*time.Millisecond)
}

// TestConsumer_RunErrorWhenAlreadyStarted 覆盖 Run 的错误分支：
// server 已在运行（Start 过）时再 Run 会返回错误（asynq 内部 Start 失败）。
func TestConsumer_RunErrorWhenAlreadyStarted(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	con := NewConsumer(testConfig(addr), nil)
	require.NoError(t, con.Start())

	err := con.Run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "taskq: consumer run")

	con.Shutdown()
}

// TestConsumer_RunUntilSignal 覆盖 Run 的阻塞成功路径：goroutine 中启动、
// 正常处理任务，收到 SIGTERM 后优雅退出并返回 nil。
//
// asynq Server.Run 依赖 OS 信号（SIGTERM/SIGINT）退出（Shutdown 不会让 Run 返回）。
// 为安全起见，测试进程自身也注册一个 SIGTERM 监听，避免信号在 asynq 注册 handler
// 之前发出导致测试进程被默认行为终止。
func TestConsumer_RunUntilSignal(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	con := NewConsumer(cfg, nil)

	// 自留 SIGTERM 通道兜底，防止信号落到默认处理器终止测试进程
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var processed int64
	con.HandleFunc("test:runsig", func(ctx context.Context, task *asynq.Task) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- con.Run() }()

	// 投递一个任务并等待其被处理：这证明 server.Start 已完成，
	// 此时 Run 已进入 waitForSignals（signal handler 已注册）。
	producer := NewProducer(cfg)
	defer producer.Close()

	_, err := producer.Enqueue(context.Background(), asynq.NewTask("test:runsig", nil))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 5*time.Second, 100*time.Millisecond)

	// 让 Run 优雅退出
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}
	assert.Equal(t, int64(1), atomic.LoadInt64(&processed))
}
