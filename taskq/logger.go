package taskq

import (
	"fmt"

	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
)

// logAdapter 将项目 logger 适配为 asynq.Logger。
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

// toAsynqConfig 将 Config 转为 asynq.Config。
// la 为 nil 时不设 Logger，asynq 使用默认日志器。
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

// redisOpt 返回 asynq Redis 连接配置。
func (c Config) redisOpt() asynq.RedisConnOpt {
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}
}

// defaultOpts 返回基于配置的默认任务选项。
func (c Config) defaultOpts() []asynq.Option {
	return []asynq.Option{
		asynq.MaxRetry(c.DefaultMaxRetry),
		asynq.Timeout(c.DefaultTimeout),
		asynq.Queue(c.DefaultQueue),
	}
}
