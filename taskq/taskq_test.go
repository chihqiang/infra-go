package taskq

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMiniRedis(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	return mr.Addr(), func() { mr.Close() }
}

func testConfig(addr string) Config {
	return Config{
		RedisAddr:       addr,
		Concurrency:     5,
		Queues:          map[string]int{"default": 1, "critical": 5},
		ShutdownTimeout: 3 * time.Second,
		DefaultMaxRetry: 3,
		DefaultTimeout:  10 * time.Second,
	}
}

// --- Config ---

func TestFillDefault(t *testing.T) {
	c := fillDefault(Config{})
	assert.Equal(t, "127.0.0.1:6379", c.RedisAddr)
	assert.Equal(t, 10, c.Concurrency)
	assert.Equal(t, 8*time.Second, c.ShutdownTimeout)
	assert.Equal(t, 25, c.DefaultMaxRetry)
	assert.Equal(t, 30*time.Minute, c.DefaultTimeout)
	assert.Equal(t, "default", c.DefaultQueue)
}

func TestFillDefault_Override(t *testing.T) {
	c := fillDefault(Config{RedisAddr: "redis:6380", Concurrency: 20, DefaultMaxRetry: 5})
	assert.Equal(t, "redis:6380", c.RedisAddr)
	assert.Equal(t, 20, c.Concurrency)
	assert.Equal(t, 5, c.DefaultMaxRetry)
}

// TestFillDefault_ExplicitZeroViaOption 验证 Config 结构体无法表达的显式 0
// 可通过 Option 设置（asynq 语义：0 次重试）。
func TestFillDefault_ExplicitZeroViaOption(t *testing.T) {
	c := fillDefault(Config{}, WithDefaultMaxRetry(0))
	assert.Equal(t, 0, c.DefaultMaxRetry, "显式 0 不应被填充为默认值 25")
}

// TestFillDefault_OptionOverridesConfig 验证 Option 优先于 Config 中的非零字段。
func TestFillDefault_OptionOverridesConfig(t *testing.T) {
	c := fillDefault(Config{DefaultMaxRetry: 5, DefaultTimeout: time.Second}, WithDefaultMaxRetry(0))
	assert.Equal(t, 0, c.DefaultMaxRetry)
	assert.Equal(t, time.Second, c.DefaultTimeout)
}

// --- Payload ---

func TestMarshalPayload(t *testing.T) {
	data := map[string]any{"k": "v"}
	b, err := MarshalPayload(data)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"k":"v"`)
}

func TestUnmarshalPayload(t *testing.T) {
	task := asynq.NewTask("t", []byte(`{"k":"v"}`))
	var m map[string]string
	require.NoError(t, UnmarshalPayload(task, &m))
	assert.Equal(t, "v", m["k"])
}

func TestUnmarshalPayload_Empty(t *testing.T) {
	task := asynq.NewTask("t", nil)
	var m map[string]string
	require.NoError(t, UnmarshalPayload(task, &m))
}

func TestUnmarshalPayload_NilTask(t *testing.T) {
	var m map[string]string
	assert.Error(t, UnmarshalPayload(nil, &m))
}

// --- Logger ---

func TestLogAdapter_Nil(t *testing.T) {
	assert.Nil(t, newLogAdapter(nil))
}

func TestLogAdapter_Delegates(t *testing.T) {
	la := newLogAdapter(logger.New(logger.Config{Output: []string{"stderr"}}))
	require.NotNil(t, la)
	assert.NotPanics(t, func() {
		la.Debug("d")
		la.Info("i")
		la.Warn("w")
		la.Error("e")
	})
}

// --- Producer ---

func TestProducer_Enqueue(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	info, err := p.Enqueue(context.Background(), asynq.NewTask("test:enqueue", []byte("{}")))
	require.NoError(t, err)
	assert.Equal(t, "test:enqueue", info.Type)
	assert.Equal(t, asynq.TaskStatePending, info.State)
}

// TestProducer_EnqueueExplicitZero 验证 Option 设置的显式 0 真正落到投递的任务上，
// 而不是被 fillDefault 替换为默认值（testConfig 里是 3）。
func TestProducer_EnqueueExplicitZero(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr), WithDefaultMaxRetry(0))
	defer p.Close()

	info, err := p.Enqueue(context.Background(), asynq.NewTask("test:zero", []byte("{}")))
	require.NoError(t, err)
	assert.Equal(t, 0, info.MaxRetry, "任务应不重试")
}

// TestProducer_EnqueueZeroTimeoutNotExpressible 锁定 asynq 的既定行为，
// 也是 taskq 不提供 WithDefaultTimeout 的原因：
// 即使把 asynq.Timeout(0) 直接传给入队，asynq 也会把它视为未设置
// 并回落 30 分钟默认超时（见 asynq.EnqueueContext 对 noTimeout 的处理）。
// 因此该 Option 给不出比 Config 更多的能力，不加不实之名不副实的 API。
func TestProducer_EnqueueZeroTimeoutNotExpressible(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(Config{RedisAddr: addr})
	defer p.Close()

	info, err := p.Enqueue(
		context.Background(),
		asynq.NewTask("test:zero-timeout", []byte("{}")),
		asynq.Timeout(0),
	)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, info.Timeout, "asynq 会把 0 超时归一化为默认 30m")
}

func TestProducer_EnqueuePayload(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	info, err := p.EnqueuePayload(context.Background(), "test:payload", map[string]string{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, "test:payload", info.Type)
}

func TestProducer_EnqueueIn(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	info, err := p.EnqueueIn(context.Background(), asynq.NewTask("test:delayed", nil), 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, asynq.TaskStateScheduled, info.State)
}

func TestProducer_EnqueueAt(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	info, err := p.EnqueueAt(context.Background(), asynq.NewTask("test:scheduled", nil), time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, asynq.TaskStateScheduled, info.State)
}

// --- Consumer ---

func TestConsumer_StartAndProcess(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	cfg.Concurrency = 2

	con := NewConsumer(cfg, nil)

	var processed int64
	con.HandleFunc("test:process", func(ctx context.Context, task *asynq.Task) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})

	require.NoError(t, con.Start())
	defer con.Shutdown()

	producer := NewProducer(cfg)
	defer producer.Close()

	for i := 0; i < 3; i++ {
		_, err := producer.Enqueue(context.Background(), asynq.NewTask("test:process", []byte("{}")))
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 3
	}, 5*time.Second, 100*time.Millisecond)

	assert.Equal(t, int64(3), atomic.LoadInt64(&processed))
}

func TestConsumer_StartTwice(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	con := NewConsumer(testConfig(addr), nil)
	defer con.Shutdown()

	require.NoError(t, con.Start())
	assert.Error(t, con.Start())
}
