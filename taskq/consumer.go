package taskq

import (
	"context"
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
)

// Consumer 消费者，封装 asynq.Server，负责拉取任务并分发给 Handler。
type Consumer struct {
	server  *asynq.Server
	mux     *asynq.ServeMux
	cfg     Config
	mu      sync.Mutex
	started bool
}

// NewConsumer 创建消费者。
// log 传 nil 使用 asynq 默认日志器。
func NewConsumer(cfg Config, log logger.ILogger) *Consumer {
	c := fillDefault(cfg)
	la := newLogAdapter(log)
	return &Consumer{
		server: asynq.NewServer(c.redisOpt(), c.toAsynqConfig(la)),
		mux:    asynq.NewServeMux(),
		cfg:    c,
	}
}

// Handle 注册任务处理器。
func (c *Consumer) Handle(pattern string, handler asynq.Handler) {
	c.mux.Handle(pattern, handler)
}

// HandleFunc 注册任务处理函数。
func (c *Consumer) HandleFunc(pattern string, handler func(context.Context, *asynq.Task) error) {
	c.mux.HandleFunc(pattern, handler)
}

// Use 添加中间件。
func (c *Consumer) Use(mws ...asynq.MiddlewareFunc) {
	c.mux.Use(mws...)
}

// Start 启动消费者（非阻塞），Shutdown 优雅关闭。
func (c *Consumer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return fmt.Errorf("taskq: consumer already running")
	}
	if err := c.server.Start(c.mux); err != nil {
		return fmt.Errorf("taskq: start consumer: %w", err)
	}
	c.started = true
	return nil
}

// Run 启动并阻塞，收到 OS 信号后优雅关闭。
func (c *Consumer) Run() error {
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	if err := c.server.Run(c.mux); err != nil {
		return fmt.Errorf("taskq: consumer run: %w", err)
	}
	return nil
}

// Shutdown 优雅关闭。
func (c *Consumer) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return
	}
	c.server.Shutdown()
	c.started = false
}
