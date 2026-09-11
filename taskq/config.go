package taskq

import (
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/mapping"
)

// --- 默认常量 ---

const (
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
	// 无法用本字段表达"不重试"：0 会被视为未设置并填充为 25。
	// 需要不重试请用 WithDefaultMaxRetry(0)。
	DefaultMaxRetry int `json:",default=25"`
	// DefaultTimeout 默认任务超时，默认 30 分钟。
	//
	// 本字段没有对应的 Option，因为 asynq 本身就无法表达"不限制超时"：
	// 其入队逻辑把 Timeout(0) 视为未设置并回落 30 分钟默认值
	// （见 asynq.EnqueueContext 对 noTimeout 的处理）。
	// 因此本字段传 0 与不传的最终结果完全相同，无需区分。
	DefaultTimeout time.Duration `json:",default=30m"`
	// DefaultQueue 默认队列名，默认 "default"。
	DefaultQueue string `json:",default=default"`
}

// Option 覆盖配置项，用于表达 Config 结构体无法表达的显式零值。
//
// 为什么需要：fillDefault 遵循「字段 == 0 视为未设置」的规则，
// 而 asynq.MaxRetry(0) 是一个有别于默认值 25 的有效取值——任务失败后不重试。
// 通过 Option 传入的 0 会在默认值填充之后应用，因此能够生效：
//
//	taskq.NewProducer(cfg, taskq.WithDefaultMaxRetry(0))
type Option func(*Config)

// WithDefaultMaxRetry 显式设置默认最大重试次数。
// 与 Config.DefaultMaxRetry 的区别：传 0 不会被填充为默认值 25，
// 而是表示 asynq.MaxRetry(0)，即任务失败后不重试。
func WithDefaultMaxRetry(n int) Option {
	return func(c *Config) { c.DefaultMaxRetry = n }
}

var fillDefaultUnmarshaler = mapping.NewDefaultUnmarshaler()

func fillDefault(cfg Config, opts ...Option) Config {
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
		// 必须拷贝：asynq 会直接持有该 map 并并发读取，
		// 与调用方共享同一实例会在调用方后续修改时构成数据竞争。
		c.Queues = make(map[string]int, len(cfg.Queues)+1)
		for name, priority := range cfg.Queues {
			c.Queues[name] = priority
		}
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

	// Option 最后应用：可覆盖上面「零值即未设置」的填充结果，
	// 从而表达 Config 结构体无法表达的显式 0（如不重试、不限制超时）。
	for _, opt := range opts {
		opt(&c)
	}

	// 保证生产者投递的队列（DefaultQueue）确实被消费者订阅。
	//
	// 若只配了 Queues 而 DefaultQueue 不在其中（默认 "default"），
	// 生产者会把任务投到一个无人消费的队列：Enqueue 返回成功、任务永久滞留，
	// 且没有任何错误提示。这里自动补上该队列，避免静默丢任务。
	c = ensureDefaultQueueConsumed(c)
	return c
}

// ensureDefaultQueueConsumed 保证 DefaultQueue 出现在消费者订阅的队列列表中。
// Queues 为空时本函数不做处理（toAsynqConfig 会自行兵底为 {DefaultQueue: 1}）。
func ensureDefaultQueueConsumed(c Config) Config {
	if len(c.Queues) == 0 {
		return c
	}
	if _, ok := c.Queues[c.DefaultQueue]; ok {
		return c
	}

	// 拷贝后再改，避免与调用方共享底层数据
	queues := make(map[string]int, len(c.Queues)+1)
	for name, priority := range c.Queues {
		queues[name] = priority
	}
	queues[c.DefaultQueue] = defaultQueuePriority
	c.Queues = queues

	logger.Warn(
		"taskq: DefaultQueue is not listed in Queues, it was added automatically to avoid unconsumed tasks",
		logger.String("default_queue", c.DefaultQueue),
	)
	return c
}
