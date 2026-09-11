# taskq

基于 [hibiken/asynq](https://github.com/hibiken/asynq) 的异步任务队列二次封装，提供生产者/消费者模式。

## 架构

```text
Producer ──投递──▶ Redis ──拉取──▶ Consumer ──分发──▶ Handler
```

## 快速开始

```go
package main

import (
    "context"
    "time"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/taskq"
    "github.com/hibiken/asynq"
)

// 任务类型名
const TaskEmailSend = "email:send"

// 任务 payload
type EmailPayload struct {
    To   string `json:"to"`
    Body string `json:"body"`
}

func main() {
    cfg := taskq.Config{
        RedisAddr:   "127.0.0.1:6379",
        Concurrency: 5,
    }

    // --- 生产者：投递任务 ---
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

    // --- 消费者：处理任务 ---
    consumer := taskq.NewConsumer(cfg, nil)
    consumer.HandleFunc(TaskEmailSend, func(ctx context.Context, task *asynq.Task) error {
        var p EmailPayload
        if err := taskq.UnmarshalPayload(task, &p); err != nil {
            return err
        }
        logger.Infof("sending email to %s: %s", p.To, p.Body)
        return nil
    })

    // 启动并阻塞，收到信号后优雅关闭
    if err := consumer.Run(); err != nil {
        logger.Fatal("consumer run failed", logger.Err(err))
    }
}
```

## 配置

```go
type Config struct {
    RedisAddr       string        // Redis 地址，默认 "127.0.0.1:6379"
    RedisPassword   string        // Redis 密码
    RedisDB         int           // Redis DB 编号

    Concurrency     int           // 消费者并发数，默认 10
    Queues          map[string]int // 队列优先级，默认 {"default": 1}
    ShutdownTimeout time.Duration // 优雅关闭超时，默认 8s

    DefaultMaxRetry int           // 默认最大重试，默认 25
    DefaultTimeout  time.Duration // 默认任务超时，默认 30m
    DefaultQueue    string        // 默认队列名，默认 "default"
}
```

### 无法用 Config 表达的零值：`DefaultMaxRetry = 0`

`fillDefault` 采用「字段 == 0 视为未设置」的规则，而 `asynq.MaxRetry(0)` 是有意义的
取值（**任务失败后不重试**）。因此 `Config{DefaultMaxRetry: 0}` 会被静默填充为 25 次重试，
与意图相反。需要显式 0 时使用 Option 形式：

```go
// 投递的任务失败后不重试（否则会被填为默认 25 次）
producer := taskq.NewProducer(cfg, taskq.WithDefaultMaxRetry(0))
consumer := taskq.NewConsumer(cfg, nil, taskq.WithDefaultMaxRetry(0))
```

| Option | 说明 |
| ------ | ------ |
| `WithDefaultMaxRetry(n)` | 显式设置默认最大重试；传 `0` 表示不重试，不会被填充为 25 |

> `DefaultTimeout` **没有**对应的 Option：asynq 自身把 `Timeout(0)` 视为未设置并回落 30 分钟
> 默认超时（见 `asynq.EnqueueContext` 对 `noTimeout` 的处理），所以传 0 与不传结果完全相同，
> 本包不提供名不副实的 API。
>
> Option 在默认值填充**之后**应用，因此会覆盖 `Config` 中的非零字段。
> 单次投递的覆盖仍可继续用 `Producer.Enqueue(ctx, task, asynq.MaxRetry(0))`。

## 延迟/定时任务

```go
// 5 分钟后执行
producer.EnqueueIn(ctx, task, 5*time.Minute)

// 指定时间执行
producer.EnqueueAt(ctx, task, time.Now().Add(2*time.Hour))
```

## 优先级队列

```go
cfg := taskq.Config{
    Queues: map[string]int{
        "critical": 6,  // 60% 的处理概率
        "default":  3,  // 30%
        "low":      1,  // 10%
    },
}

// 投递到指定队列
producer.Enqueue(ctx, task, asynq.Queue("critical"))
```

## 中间件

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

## 集成项目日志

```go
log := logger.New(logger.Config{Encoding: logger.JSONEncoding})
consumer := taskq.NewConsumer(cfg, log) // asynq 内部日志走项目 logger
```
