package taskq

import (
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/mapping"
)

// --- Default constants ---

const (
	// defaultQueuePriority is the default queue priority.
	defaultQueuePriority = 1
)

// Config is the task queue configuration.
type Config struct {
	// RedisAddr is the Redis address, default "127.0.0.1:6379".
	RedisAddr string `json:",default=127.0.0.1:6379"`
	// RedisPassword is the Redis password, empty by default.
	RedisPassword string `json:",optional"`
	// RedisDB is the Redis database number, 0 by default.
	RedisDB int `json:",optional"`

	// Concurrency is the number of concurrent consumers, 10 by default.
	Concurrency int `json:",default=10"`
	// Queues maps queue names to priorities, {"default": 1} by default.
	Queues map[string]int `json:",optional"`
	// ShutdownTimeout is the graceful shutdown timeout, 8 seconds by default.
	ShutdownTimeout time.Duration `json:",default=8s"`

	// DefaultMaxRetry is the default maximum number of retries, 25 by default.
	// This field cannot express "no retry": 0 is treated as unset and filled with 25.
	// Use WithDefaultMaxRetry(0) when no retry is wanted.
	DefaultMaxRetry int `json:",default=25"`
	// DefaultTimeout is the default task timeout, 30 minutes by default.
	//
	// This field has no matching Option, because asynq itself cannot express
	// "no timeout": its enqueue logic treats Timeout(0) as unset and falls back to
	// the 30 minute default (see how asynq.EnqueueContext handles noTimeout).
	// Passing 0 or leaving the field unset therefore yields exactly the same
	// result, so the two cases need no distinction.
	DefaultTimeout time.Duration `json:",default=30m"`
	// DefaultQueue is the default queue name, "default" by default.
	DefaultQueue string `json:",default=default"`
}

// Option overrides a configuration entry; it is used to express explicit zero
// values that the Config struct cannot represent.
//
// Why it is needed: fillDefault follows the rule "field == 0 means unset", while
// asynq.MaxRetry(0) is a valid value distinct from the default 25 -- no retry
// after a task fails. A 0 passed through an Option is applied after the defaults
// are filled in, so it does take effect:
//
//	taskq.NewProducer(cfg, taskq.WithDefaultMaxRetry(0))
type Option func(*Config)

// WithDefaultMaxRetry explicitly sets the default maximum number of retries.
// The difference from Config.DefaultMaxRetry: passing 0 is not filled with the
// default 25 but means asynq.MaxRetry(0), that is, no retry after a task fails.
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
		// A copy is mandatory: asynq holds this map directly and reads it
		// concurrently, so sharing the same instance with the caller would become a
		// data race once the caller mutates it.
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

	// Options are applied last: they may override the "zero means unset" results
	// filled in above, and thus express an explicit 0 (such as no retry or no
	// timeout limit) that the Config struct cannot represent.
	for _, opt := range opts {
		opt(&c)
	}

	// Make sure the queue the producer enqueues to (DefaultQueue) is actually
	// subscribed by the consumer.
	//
	// If only Queues is set and DefaultQueue is not part of it (the default is
	// "default"), the producer would enqueue tasks to a queue nobody consumes:
	// Enqueue returns success, the tasks stay there forever, and no error is
	// reported. Add that queue automatically here to avoid silently losing tasks.
	c = ensureDefaultQueueConsumed(c)
	return c
}

// ensureDefaultQueueConsumed makes sure DefaultQueue appears in the list of
// queues the consumer subscribes to.
// When Queues is empty this function does nothing (toAsynqConfig falls back to
// {DefaultQueue: 1} on its own).
func ensureDefaultQueueConsumed(c Config) Config {
	if len(c.Queues) == 0 {
		return c
	}
	if _, ok := c.Queues[c.DefaultQueue]; ok {
		return c
	}

	// Copy before mutating, to avoid sharing the underlying data with the caller
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
