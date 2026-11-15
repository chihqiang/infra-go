package taskq

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Producer wraps asynq.Client and is responsible for enqueueing tasks onto queues.
type Producer struct {
	client      *asynq.Client
	cfg         Config
	defaultOpts []asynq.Option // default options cached at construction, so enqueues do not rebuild them
}

// NewProducer creates a producer.
// opts expresses explicit zero values that the Config struct cannot represent, see Option.
func NewProducer(cfg Config, opts ...Option) *Producer {
	c := fillDefault(cfg, opts...)
	return &Producer{
		client:      asynq.NewClient(c.redisOpt()),
		cfg:         c,
		defaultOpts: c.defaultOpts(),
	}
}

// Close closes the producer and releases the connection.
func (p *Producer) Close() error { return p.client.Close() }

// Enqueue enqueues a task for immediate execution.
// opts can override the default options (queue, retries, timeout, and so on).
func (p *Producer) Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	// Copy first: appending directly could reuse the backing array of defaultOpts
	// and leak state across calls.
	opts = append(append([]asynq.Option{}, p.defaultOpts...), opts...)
	info, err := p.client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		return nil, fmt.Errorf("taskq: enqueue %q: %w", task.Type(), err)
	}
	return info, nil
}

// EnqueuePayload enqueues a task and marshals the payload to JSON automatically.
func (p *Producer) EnqueuePayload(ctx context.Context, typename string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	data, err := MarshalPayload(payload)
	if err != nil {
		return nil, err
	}
	return p.Enqueue(ctx, asynq.NewTask(typename, data), opts...)
}

// EnqueueIn enqueues a task for delayed execution, running after d.
func (p *Producer) EnqueueIn(ctx context.Context, task *asynq.Task, d time.Duration, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessIn(d))
	return p.Enqueue(ctx, task, opts...)
}

// EnqueueAt enqueues a task for scheduled execution at time t.
func (p *Producer) EnqueueAt(ctx context.Context, task *asynq.Task, t time.Time, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessAt(t))
	return p.Enqueue(ctx, task, opts...)
}
