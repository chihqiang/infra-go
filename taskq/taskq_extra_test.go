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

// This file adds deterministic coverage for the remaining small branches:
//   - Shutdown called before start (idempotent)
//   - NewConsumer with a non-nil logger (the branch in toAsynqConfig that sets Logger)
//   - the remaining fillDefault branches that keep values
//     (RedisDB/ShutdownTimeout/DefaultTimeout/DefaultQueue)
//   - the marshal/unmarshal error paths of MarshalPayload/UnmarshalPayload

// TestConsumer_ShutdownBeforeStart covers Shutdown before start (idempotent, no panic).
func TestConsumer_ShutdownBeforeStart(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	con := NewConsumer(testConfig(addr), nil)

	// Shut down directly without starting, and shutting down again is still safe
	con.Shutdown()
	con.Shutdown()

	// It can still be started normally after being shut down
	require.NoError(t, con.Start())
	assert.True(t, con.started)
	con.Shutdown()
	assert.False(t, con.started)
}

// TestNewConsumer_WithLogger covers NewConsumer with a non-nil logger
// (the branch in toAsynqConfig where la != nil sets the asynq Logger).
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

// TestFillDefault_RemainingFields covers the fillDefault branches that keep
// non-zero values for the remaining fields
// (RedisDB/ShutdownTimeout/DefaultTimeout/DefaultQueue).
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

// TestProducer_EnqueuePayloadMarshalError covers the marshal failure branch of
// EnqueuePayload / MarshalPayload (a payload holding a value that JSON cannot marshal).
func TestProducer_EnqueuePayloadMarshalError(t *testing.T) {
	addr, cleanup := newMiniRedis(t)
	defer cleanup()

	p := NewProducer(testConfig(addr))
	defer p.Close()

	_, err := p.EnqueuePayload(context.Background(), "bad:payload", make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal payload")
}

// TestUnmarshalPayload_InvalidJSON covers the unmarshal failure branch of UnmarshalPayload.
func TestUnmarshalPayload_InvalidJSON(t *testing.T) {
	task := asynq.NewTask("t", []byte("{not-json"))
	var m map[string]string
	err := UnmarshalPayload(task, &m)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal payload")
}
