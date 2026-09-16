package taskq

import (
	"context"
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
	"github.com/hibiken/asynq"
)

// Consumer wraps asynq.Server; it pulls tasks and dispatches them to Handlers.
type Consumer struct {
	server  *asynq.Server
	mux     *asynq.ServeMux
	cfg     Config
	mu      sync.Mutex
	started bool
}

// NewConsumer creates a consumer.
// Passing nil for log uses the default asynq logger.
// opts expresses explicit zero values that the Config struct cannot represent, see Option.
func NewConsumer(cfg Config, log logger.ILogger, opts ...Option) *Consumer {
	c := fillDefault(cfg, opts...)
	la := newLogAdapter(log)
	return &Consumer{
		server: asynq.NewServer(c.redisOpt(), c.toAsynqConfig(la)),
		mux:    asynq.NewServeMux(),
		cfg:    c,
	}
}

// Handle registers a task handler.
func (c *Consumer) Handle(pattern string, handler asynq.Handler) {
	c.mux.Handle(pattern, handler)
}

// HandleFunc registers a task handler function.
func (c *Consumer) HandleFunc(pattern string, handler func(context.Context, *asynq.Task) error) {
	c.mux.HandleFunc(pattern, handler)
}

// Use adds middleware.
func (c *Consumer) Use(mws ...asynq.MiddlewareFunc) {
	c.mux.Use(mws...)
}

// Start starts the consumer without blocking; use Shutdown for a graceful stop.
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

// Run starts the consumer and blocks until an OS signal triggers a graceful stop.
func (c *Consumer) Run() error {
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	if err := c.server.Run(c.mux); err != nil {
		return fmt.Errorf("taskq: consumer run: %w", err)
	}
	return nil
}

// Shutdown performs a graceful shutdown.
func (c *Consumer) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return
	}
	c.server.Shutdown()
	c.started = false
}
