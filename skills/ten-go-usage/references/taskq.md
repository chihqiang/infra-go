# taskq

A wrapper around [hibiken/asynq](https://github.com/hibiken/asynq) providing a producer/consumer
async task queue.

## Architecture

```text
Producer ──enqueue──▶ Redis ──fetch──▶ Consumer ──dispatch──▶ Handler
```

## Quick start

```go
package main

import (
    "context"
    "time"

    "github.com/chihqiang/ten-go/core/logger"
    "github.com/chihqiang/ten-go/core/taskq"
    "github.com/hibiken/asynq"
)

// task type name
const TaskEmailSend = "email:send"

// task payload
type EmailPayload struct {
    To   string `json:"to"`
    Body string `json:"body"`
}

func main() {
    cfg := taskq.Config{
        RedisAddr:   "127.0.0.1:6379",
        Concurrency: 5,
    }

    // --- producer: enqueue tasks ---
    producer := taskq.NewProducer(cfg)
    defer producer.Close()

    info, err := producer.EnqueuePayload(context.Background(), TaskEmailSend, EmailPayload{
        To:   "user@example.com",
        Body: "Hello!",
    })
    if err != nil {
        logger.Fatal("failed to enqueue task", logger.Err(err))
    }
    logger.Info("task enqueued", logger.String("id", info.ID))

    // --- consumer: process tasks ---
    consumer := taskq.NewConsumer(cfg, nil)
    consumer.HandleFunc(TaskEmailSend, func(ctx context.Context, task *asynq.Task) error {
        var p EmailPayload
        if err := taskq.UnmarshalPayload(task, &p); err != nil {
            return err
        }
        logger.Infof("sending email to %s: %s", p.To, p.Body)
        return nil
    })

    // run and block, shutting down gracefully once a signal arrives
    if err := consumer.Run(); err != nil {
        logger.Fatal("consumer run failed", logger.Err(err))
    }
}
```

## Configuration

```go
type Config struct {
    RedisAddr       string        // Redis address, default "127.0.0.1:6379"
    RedisPassword   string        // Redis password
    RedisDB         int           // Redis DB number

    Concurrency     int           // consumer concurrency, default 10
    Queues          map[string]int // queue priorities, default {"default": 1}
    ShutdownTimeout time.Duration // graceful shutdown timeout, default 8s

    DefaultMaxRetry int           // default max retries, default 25
    DefaultTimeout  time.Duration // default task timeout, default 30m
    DefaultQueue    string        // default queue name, default "default"

    StrictPriority bool                        // strict queue priority (default false: weighted)
    IsFailure      func(error) bool            // nil: every non-nil error is a failure
    RetryDelayFunc taskq.RetryDelayFunc        // nil: asynq's exponential backoff
}
```

### Retry policy

`Config.DefaultMaxRetry` (25 by default) is applied through `asynq.MaxRetry`. With asynq's
default exponential backoff a permanently failing task stays in the queue for roughly **25
days** (`sum(n^4+15)` over 25 attempts). Tune the policy instead of relying on the default:

```go
cfg := taskq.Config{
    DefaultMaxRetry: 5, // fail fast for transient workloads
    IsFailure: func(err error) bool {
        // context cancellation during shutdown is expected, not a failure
        return !errors.Is(err, context.Canceled)
    },
    RetryDelayFunc: func(n int, err error, t *asynq.Task) time.Duration {
        // cap the default backoff at one minute
        return min(taskq.DefaultRetryDelayFunc(n, err, t), time.Minute)
    },
    StrictPriority: true, // highest-priority queue is always served first
}
```

Handlers should return a wrapped `taskq.SkipRetry` for permanent failures (invalid payload,
business validation) so asynq archives the task instead of retrying it:

```go
if err := validate(task); err != nil {
    return fmt.Errorf("invalid payload: %w", taskq.SkipRetry)
}
```

`taskq.SkipRetry`, `taskq.RetryDelayFunc` and `taskq.DefaultRetryDelayFunc` are re-exports, so
the asynq package is not required in application code.

### Zero values Config cannot express: `DefaultMaxRetry = 0`

`fillDefault` follows the "field == 0 means unset" rule, whereas `asynq.MaxRetry(0)` is a
meaningful value (**do not retry after a failure**). `Config{DefaultMaxRetry: 0}` is therefore
silently filled in with 25 retries, the opposite of the intent. Use the Option form when you need
an explicit 0:

```go
// enqueued tasks are not retried after a failure (otherwise it would be filled in as 25)
producer := taskq.NewProducer(cfg, taskq.WithDefaultMaxRetry(0))
consumer := taskq.NewConsumer(cfg, nil, taskq.WithDefaultMaxRetry(0))
```

| Option | Description |
| ------ | ------ |
| `WithDefaultMaxRetry(n)` | Set the default max retries explicitly; `0` means no retry and is not filled in as 25 |

> `DefaultTimeout` has **no** corresponding Option: asynq itself treats `Timeout(0)` as unset and
> falls back to the 30-minute default timeout (see how `asynq.EnqueueContext` handles `noTimeout`),
> so passing 0 and passing nothing produce identical results, and this package does not ship an API
> that does not do what its name says.
>
> Options are applied **after** the defaults are filled in, so they override the non-zero fields in
> `Config`. Per-enqueue overrides can still use `Producer.Enqueue(ctx, task, asynq.MaxRetry(0))`.

## Delayed/scheduled tasks

```go
// run 5 minutes later
producer.EnqueueIn(ctx, task, 5*time.Minute)

// run at a specific time
producer.EnqueueAt(ctx, task, time.Now().Add(2*time.Hour))
```

## Priority queues

```go
cfg := taskq.Config{
    Queues: map[string]int{
        "critical": 6,  // 60% chance of being processed
        "default":  3,  // 30%
        "low":      1,  // 10%
    },
}

// enqueue into a specific queue
producer.Enqueue(ctx, task, asynq.Queue("critical"))
```

## Middleware

```go
consumer := taskq.NewConsumer(cfg, nil)
consumer.Use(func(next asynq.Handler) asynq.Handler {
    return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
        logger.Infof("start: %s", task.Type())
        err := next.ProcessTask(ctx, task)
        logger.Infof("done: %s, err: %v", task.Type(), err)
        return err
    })
})
consumer.HandleFunc("my:task", handler)
```

## Integrating the project logger

```go
log := logger.New(logger.Config{Encoding: logger.JSONEncoding})
consumer := taskq.NewConsumer(cfg, log) // asynq's internal logs go through the project logger
```
