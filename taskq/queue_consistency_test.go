package taskq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖队列配置一致性（回归）：
// DefaultQueue 必须出现在消费者订阅的 Queues 中，
// 否则生产者会把任务投进无人消费的队列，任务永久滞留且无任何错误提示。

// TestFillDefault_EnsuresDefaultQueueConsumed 回归测试：
// 只配 Queues、DefaultQueue 保留默认值时，必须自动补上 DefaultQueue。
func TestFillDefault_EnsuresDefaultQueueConsumed(t *testing.T) {
	c := fillDefault(Config{
		Queues: map[string]int{"critical": 6, "low": 1},
		// DefaultQueue 未设置 → 默认 "default"
	})

	assert.Equal(t, "default", c.DefaultQueue)
	// 关键：DefaultQueue 必须在 Queues 中，否则任务无人消费
	priority, ok := c.Queues["default"]
	require.True(t, ok, "DefaultQueue must be present in Queues, got %v", c.Queues)
	assert.Equal(t, defaultQueuePriority, priority)

	// 用户配置的队列保持不变
	assert.Equal(t, 6, c.Queues["critical"])
	assert.Equal(t, 1, c.Queues["low"])
}

// TestFillDefault_KeepsExplicitDefaultQueueInQueues 验证用户已把 DefaultQueue
// 纳入 Queues 时不改变其优先级。
func TestFillDefault_KeepsExplicitDefaultQueueInQueues(t *testing.T) {
	c := fillDefault(Config{
		Queues:       map[string]int{"default": 5, "critical": 9},
		DefaultQueue: "default",
	})

	assert.Equal(t, 5, c.Queues["default"], "explicit priority must be preserved")
	assert.Equal(t, 9, c.Queues["critical"])
}

// TestFillDefault_CustomDefaultQueueEnsured 验证自定义 DefaultQueue 同样被补入。
func TestFillDefault_CustomDefaultQueueEnsured(t *testing.T) {
	c := fillDefault(Config{
		Queues:       map[string]int{"critical": 6},
		DefaultQueue: "critical",
	})

	_, ok := c.Queues["critical"]
	assert.True(t, ok)
	assert.Equal(t, "critical", c.DefaultQueue)
}

// TestFillDefault_EmptyQueuesUntouched 验证未配置 Queues 时不做改动
// （由 toAsynqConfig 兜底为 {DefaultQueue: 1}）。
func TestFillDefault_EmptyQueuesUntouched(t *testing.T) {
	c := fillDefault(Config{})
	assert.Empty(t, c.Queues)
	assert.Equal(t, "default", c.DefaultQueue)
}

// TestFillDefault_QueuesIsCopied 回归测试：Queues 必须被拷贝，不能与调用方共享。
//
// 历史缺陷：c.Queues = cfg.Queues 直接别名，而 asynq 会持有该 map 并并发读取，
// 调用方构造后修改 map 会构成数据竞争 / 队列权重不一致。
func TestFillDefault_QueuesIsCopied(t *testing.T) {
	original := map[string]int{"critical": 6}
	c := fillDefault(Config{Queues: original, DefaultQueue: "critical"})

	// 修改原始 map 不应影响填充后的配置
	original["critical"] = 99
	original["new"] = 1

	assert.Equal(t, 6, c.Queues["critical"], "config must not alias the caller's map")
	_, leaked := c.Queues["new"]
	assert.False(t, leaked, "later mutations of the caller's map must not leak in")
}

// TestDefaultQueueIsConsumedByAsynqConfig 验证 toAsynqConfig 输出的队列集合
// 一定包含 DefaultQueue（生产者与消费者的一致性）。
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

// TestDefaultOpts_RoutesToConsumedQueue 验证 defaultOpts 指定的队列确实被订阅。
func TestDefaultOpts_RoutesToConsumedQueue(t *testing.T) {
	c := fillDefault(Config{Queues: map[string]int{"critical": 6}})

	asynqCfg := c.toAsynqConfig(nil)
	_, consumed := asynqCfg.Queues[c.DefaultQueue]
	require.True(t, consumed,
		"defaultOpts enqueues to %q but consumed queues are %v",
		c.DefaultQueue, asynqCfg.Queues)

	// defaultOpts 非空且包含队列选项
	assert.NotEmpty(t, c.defaultOpts())
}
