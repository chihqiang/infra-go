package taskq

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Producer 生产者，封装 asynq.Client，负责投递任务到队列。
type Producer struct {
	client *asynq.Client
	cfg    Config
}

// NewProducer 创建生产者。
func NewProducer(cfg Config) *Producer {
	c := fillDefault(cfg)
	return &Producer{
		client: asynq.NewClient(c.redisOpt()),
		cfg:    c,
	}
}

// Close 关闭，释放连接。
func (p *Producer) Close() error { return p.client.Close() }

// Enqueue 投递任务到队列立即执行。
// opts 可覆盖默认选项（队列、重试、超时等）。
func (p *Producer) Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(p.cfg.defaultOpts(), opts...)
	info, err := p.client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		return nil, fmt.Errorf("taskq: enqueue %q: %w", task.Type(), err)
	}
	return info, nil
}

// EnqueuePayload 投递任务，自动将 payload JSON 序列化。
func (p *Producer) EnqueuePayload(ctx context.Context, typename string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	data, err := MarshalPayload(payload)
	if err != nil {
		return nil, err
	}
	return p.Enqueue(ctx, asynq.NewTask(typename, data), opts...)
}

// EnqueueIn 延迟投递，d 后执行。
func (p *Producer) EnqueueIn(ctx context.Context, task *asynq.Task, d time.Duration, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessIn(d))
	return p.Enqueue(ctx, task, opts...)
}

// EnqueueAt 定时投递，在 t 时刻执行。
func (p *Producer) EnqueueAt(ctx context.Context, task *asynq.Task, t time.Time, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessAt(t))
	return p.Enqueue(ctx, task, opts...)
}
