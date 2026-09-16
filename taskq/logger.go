package taskq

import (
	"fmt"

	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
)

// logAdapter adapts the project logger to asynq.Logger.
type logAdapter struct{ log logger.ILogger }

func newLogAdapter(log logger.ILogger) *logAdapter {
	if log == nil {
		return nil
	}
	return &logAdapter{log: log}
}

func (a *logAdapter) Debug(args ...interface{}) { a.log.Debug(fmt.Sprint(args...)) }
func (a *logAdapter) Info(args ...interface{})  { a.log.Info(fmt.Sprint(args...)) }
func (a *logAdapter) Warn(args ...interface{})  { a.log.Warn(fmt.Sprint(args...)) }
func (a *logAdapter) Error(args ...interface{}) { a.log.Error(fmt.Sprint(args...)) }
func (a *logAdapter) Fatal(args ...interface{}) { a.log.Fatal(fmt.Sprint(args...)) }

// toAsynqConfig converts Config into asynq.Config.
// When la is nil no Logger is set and asynq uses its default logger.
func (c Config) toAsynqConfig(la *logAdapter) asynq.Config {
	queues := c.Queues
	if len(queues) == 0 {
		queues = map[string]int{c.DefaultQueue: defaultQueuePriority}
	}
	cfg := asynq.Config{
		Concurrency:     c.Concurrency,
		Queues:          queues,
		ShutdownTimeout: c.ShutdownTimeout,
		LogLevel:        asynq.InfoLevel,
	}
	if la != nil {
		cfg.Logger = la
	}
	return cfg
}

// redisOpt returns the asynq Redis connection configuration.
func (c Config) redisOpt() asynq.RedisConnOpt {
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}
}

// defaultOpts returns the default task options derived from the configuration.
func (c Config) defaultOpts() []asynq.Option {
	return []asynq.Option{
		asynq.MaxRetry(c.DefaultMaxRetry),
		asynq.Timeout(c.DefaultTimeout),
		asynq.Queue(c.DefaultQueue),
	}
}
