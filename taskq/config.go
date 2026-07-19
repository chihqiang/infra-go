package taskq

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// --- 默认常量 ---

const (
	// defaultRedisAddr 默认 Redis 地址。
	defaultRedisAddr = "127.0.0.1:6379"
	// defaultConcurrency 默认消费者并发数。
	defaultConcurrency = 10
	// defaultShutdownTimeout 默认优雅关闭超时。
	defaultShutdownTimeout = 8 * time.Second
	// defaultMaxRetry 默认最大重试次数。
	defaultMaxRetry = 25
	// defaultTimeout 默认任务超时。
	defaultTimeout = 30 * time.Minute
	// defaultQueueName 默认队列名。
	defaultQueueName = "default"
	// defaultQueuePriority 默认队列优先级。
	defaultQueuePriority = 1
)

// Config 任务队列配置。
type Config struct {
	// RedisAddr Redis 地址，默认 "127.0.0.1:6379"。
	RedisAddr string `json:",default=127.0.0.1:6379"`
	// RedisPassword Redis 密码，默认空。
	RedisPassword string `json:",optional"`
	// RedisDB Redis 数据库编号，默认 0。
	RedisDB int `json:",optional"`

	// Concurrency 消费者并发数，默认 10。
	Concurrency int `json:",default=10"`
	// Queues 队列名与优先级映射，默认 {"default": 1}。
	Queues map[string]int `json:",optional"`
	// ShutdownTimeout 优雅关闭超时，默认 8 秒。
	ShutdownTimeout time.Duration `json:",default=8s"`

	// DefaultMaxRetry 默认最大重试次数，默认 25。
	DefaultMaxRetry int `json:",default=25"`
	// DefaultTimeout 默认任务超时，默认 30 分钟。
	DefaultTimeout time.Duration `json:",default=30m"`
	// DefaultQueue 默认队列名，默认 "default"。
	DefaultQueue string `json:",default=default"`
}

var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}
	if cfg.RedisAddr != "" {
		c.RedisAddr = cfg.RedisAddr
	}
	c.RedisPassword = cfg.RedisPassword
	if cfg.RedisDB != 0 {
		c.RedisDB = cfg.RedisDB
	}
	if cfg.Concurrency != 0 {
		c.Concurrency = cfg.Concurrency
	}
	if len(cfg.Queues) > 0 {
		c.Queues = cfg.Queues
	}
	if cfg.ShutdownTimeout != 0 {
		c.ShutdownTimeout = cfg.ShutdownTimeout
	}
	if cfg.DefaultMaxRetry != 0 {
		c.DefaultMaxRetry = cfg.DefaultMaxRetry
	}
	if cfg.DefaultTimeout != 0 {
		c.DefaultTimeout = cfg.DefaultTimeout
	}
	if cfg.DefaultQueue != "" {
		c.DefaultQueue = cfg.DefaultQueue
	}
	return c
}
