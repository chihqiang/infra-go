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

// This file covers the Consumer APIs not exercised before:
//   - Handle (registering an asynq.Handler object)
//   - Use (middleware chain)
//   - Run (blocking run: error branch plus signal driven exit)
//
// It reuses the helpers from taskq_test.go: newMiniRedis/testConfig.

// countingTaskHandler implements the asynq.Handler interface for the Handle tests.
type countingTaskHandler struct {
	count *int64
}

func (h *countingTaskHandler) ProcessTask(_ context.Context, _ *asynq.Task) error {
	atomic.AddInt64(h.count, 1)
	return nil
}

// TestConsumer_HandleAndUse verifies end to end that Handle (Handler object) and
// Use (middleware) registration take effect.
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

	// Use: middleware runs before the handler and can inspect/rewrite the task
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

	// The middleware ran as many times as there were tasks, and it saw the task type
	assert.Equal(t, int64(3), atomic.LoadInt64(&mwCalls))
	assert.Equal(t, "test:handle", lastTask.Load().(string))
}

// TestConsumer_HandleWithDifferentPatterns verifies that Handle registers
// different prefix patterns and each one routes precisely to its own handler
// (the asynq mux matches the longest prefix).
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

// TestConsumer_RunErrorWhenAlreadyStarted covers the error branch of Run:
// calling Run while the server is already running (Start was called) returns an
// error (asynq fails its internal Start).
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

// TestConsumer_RunUntilSignal covers the blocking success path of Run: start it
// in a goroutine, process tasks normally, and after receiving SIGTERM it shuts
// down gracefully and returns nil.
//
// asynq Server.Run exits on an OS signal (SIGTERM/SIGINT); Shutdown alone does not
// make Run return. To be safe, the test process registers its own SIGTERM
// listener, so a signal delivered before asynq registers its handler does not
// terminate the test process through the default behaviour.
func TestConsumer_RunUntilSignal(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	con := NewConsumer(cfg, nil)

	// Keep a SIGTERM channel as a safety net, so the signal does not reach the
	// default handler and terminate the test process
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

	// Enqueue one task and wait for it to be processed: this proves that
	// server.Start has finished, so Run has already reached waitForSignals
	// (the signal handler is registered).
	producer := NewProducer(cfg)
	defer producer.Close()

	_, err := producer.Enqueue(context.Background(), asynq.NewTask("test:runsig", nil))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 5*time.Second, 100*time.Millisecond)

	// Let Run exit gracefully
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
