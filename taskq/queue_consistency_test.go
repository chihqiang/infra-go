package taskq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers queue configuration consistency (regression):
// DefaultQueue must appear in the Queues the consumer subscribes to, otherwise
// the producer enqueues tasks to a queue nobody consumes, where they stay forever
// without any error being reported.

// TestFillDefault_EnsuresDefaultQueueConsumed is a regression test: when only
// Queues is set and DefaultQueue keeps its default value, DefaultQueue must be
// added automatically.
func TestFillDefault_EnsuresDefaultQueueConsumed(t *testing.T) {
	c := fillDefault(Config{
		Queues: map[string]int{"critical": 6, "low": 1},
		// DefaultQueue is not set -> defaults to "default"
	})

	assert.Equal(t, "default", c.DefaultQueue)
	// Key point: DefaultQueue must be in Queues, otherwise no one consumes the tasks
	priority, ok := c.Queues["default"]
	require.True(t, ok, "DefaultQueue must be present in Queues, got %v", c.Queues)
	assert.Equal(t, defaultQueuePriority, priority)

	// The queues configured by the user stay unchanged
	assert.Equal(t, 6, c.Queues["critical"])
	assert.Equal(t, 1, c.Queues["low"])
}

// TestFillDefault_KeepsExplicitDefaultQueueInQueues verifies that when the user
// already lists DefaultQueue in Queues its priority is left untouched.
func TestFillDefault_KeepsExplicitDefaultQueueInQueues(t *testing.T) {
	c := fillDefault(Config{
		Queues:       map[string]int{"default": 5, "critical": 9},
		DefaultQueue: "default",
	})

	assert.Equal(t, 5, c.Queues["default"], "explicit priority must be preserved")
	assert.Equal(t, 9, c.Queues["critical"])
}

// TestFillDefault_CustomDefaultQueueEnsured verifies that a custom DefaultQueue is added as well.
func TestFillDefault_CustomDefaultQueueEnsured(t *testing.T) {
	c := fillDefault(Config{
		Queues:       map[string]int{"critical": 6},
		DefaultQueue: "critical",
	})

	_, ok := c.Queues["critical"]
	assert.True(t, ok)
	assert.Equal(t, "critical", c.DefaultQueue)
}

// TestFillDefault_EmptyQueuesUntouched verifies that nothing is changed when
// Queues is not configured (toAsynqConfig falls back to {DefaultQueue: 1}).
func TestFillDefault_EmptyQueuesUntouched(t *testing.T) {
	c := fillDefault(Config{})
	assert.Empty(t, c.Queues)
	assert.Equal(t, "default", c.DefaultQueue)
}

// TestFillDefault_QueuesIsCopied is a regression test: Queues must be copied and
// must not be shared with the caller.
//
// Historical defect: c.Queues = cfg.Queues aliased the map directly, while asynq
// holds that map and reads it concurrently, so mutating the map after building the
// caller's config caused a data race and inconsistent queue weights.
func TestFillDefault_QueuesIsCopied(t *testing.T) {
	original := map[string]int{"critical": 6}
	c := fillDefault(Config{Queues: original, DefaultQueue: "critical"})

	// Mutating the original map must not affect the filled configuration
	original["critical"] = 99
	original["new"] = 1

	assert.Equal(t, 6, c.Queues["critical"], "config must not alias the caller's map")
	_, leaked := c.Queues["new"]
	assert.False(t, leaked, "later mutations of the caller's map must not leak in")
}

// TestDefaultQueueIsConsumedByAsynqConfig verifies that the set of queues produced
// by toAsynqConfig always includes DefaultQueue (producer/consumer consistency).
func TestDefaultQueueIsConsumedByAsynqConfig(t *testing.T) {
	c := fillDefault(Config{
		Queues:       map[string]int{"critical": 6},
		DefaultQueue: "default",
	})

	asynqCfg := c.toAsynqConfig(nil)
	_, ok := asynqCfg.Queues[c.DefaultQueue]
	assert.True(t, ok,
		"the queue tasks are enqueued to (%q) must be among the consumed queues %v",
		c.DefaultQueue, asynqCfg.Queues)
}

// TestDefaultOpts_RoutesToConsumedQueue verifies that the queue chosen by
// defaultOpts really is subscribed.
func TestDefaultOpts_RoutesToConsumedQueue(t *testing.T) {
	c := fillDefault(Config{Queues: map[string]int{"critical": 6}})

	asynqCfg := c.toAsynqConfig(nil)
	_, consumed := asynqCfg.Queues[c.DefaultQueue]
	require.True(t, consumed,
		"defaultOpts enqueues to %q but consumed queues are %v",
		c.DefaultQueue, asynqCfg.Queues)

	// defaultOpts is non-empty and contains a queue option
	assert.NotEmpty(t, c.defaultOpts())
}
