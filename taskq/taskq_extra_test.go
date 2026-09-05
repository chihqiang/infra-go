package taskq

import (
	"context"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件补充剩余小分支的确定性覆盖：
//   - Shutdown 未启动即调用（幂等）
//   - NewConsumer 传入非 nil logger（toAsynqConfig 设置 Logger 分支）
//   - fillDefault 剩余字段保留分支（RedisDB/ShutdownTimeout/DefaultTimeout/DefaultQueue）
//   - MarshalPayload/UnmarshalPayload 的序列化/反序列化错误路径

// TestConsumer_ShutdownBeforeStart 覆盖未启动即 Shutdown（幂等、不 panic）。
func TestConsumer_ShutdownBeforeStart(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	con := NewConsumer(testConfig(addr), nil)

	// 未启动直接关闭，再关一次仍安全
	con.Shutdown()
	con.Shutdown()

	// 关闭后仍可正常启动
	require.NoError(t, con.Start())
	assert.True(t, con.started)
	con.Shutdown()
	assert.False(t, con.started)
}

// TestNewConsumer_WithLogger 覆盖 NewConsumer 传入非 nil logger
// （toAsynqConfig 中 la != nil 会设置 asynq Logger 的分支）。
func TestNewConsumer_WithLogger(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	cfg := testConfig(addr)
	con := NewConsumer(cfg, logger.New(logger.Config{Output: []string{"stderr"}}))
	require.NotNil(t, con.server)

	require.NoError(t, con.Start())
	defer con.Shutdown()
	assert.True(t, con.started)
}

// TestFillDefault_RemainingFields 覆盖 fillDefault 剩余字段的非零保留分支
// （RedisDB/ShutdownTimeout/DefaultTimeout/DefaultQueue）。
func TestFillDefault_RemainingFields(t *testing.T) {
	c := fillDefault(Config{
		RedisDB:         3,
		ShutdownTimeout: 20 * time.Second,
		DefaultTimeout:  5 * time.Minute,
		DefaultQueue:    "critical",
	})
	assert.Equal(t, 3, c.RedisDB)
	assert.Equal(t, 20*time.Second, c.ShutdownTimeout)
	assert.Equal(t, 5*time.Minute, c.DefaultTimeout)
	assert.Equal(t, "critical", c.DefaultQueue)
}

// TestProducer_EnqueuePayloadMarshalError 覆盖 EnqueuePayload / MarshalPayload
// 的序列化失败分支（payload 含不可 JSON 序列化值）。
func TestProducer_EnqueuePayloadMarshalError(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	_, err := p.EnqueuePayload(context.Background(), "bad:payload", make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal payload")
}

// TestUnmarshalPayload_InvalidJSON 覆盖 UnmarshalPayload 反序列化失败分支。
func TestUnmarshalPayload_InvalidJSON(t *testing.T) {
	task := asynq.NewTask("t", []byte("{not-json"))
	var m map[string]string
	err := UnmarshalPayload(task, &m)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal payload")
}
