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
    "log"
    "time"

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
        log.Fatal(err)
    }
    log.Printf("task enqueued: %s", info.ID)

    // --- 消费者：处理任务 ---
    consumer := taskq.NewConsumer(cfg, nil)
    consumer.HandleFunc(TaskEmailSend, func(ctx context.Context, task *asynq.Task) error {
        var p EmailPayload
        if err := taskq.UnmarshalPayload(task, &p); err != nil {
            return err
        }
        log.Printf("sending email to %s: %s", p.To, p.Body)
        return nil
    })

    // 启动并阻塞，收到信号后优雅关闭
    if err := consumer.Run(); err != nil {
        log.Fatal(err)
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
        log.Printf("start: %s", task.Type())
        err := next.ProcessTask(ctx, task)
        log.Printf("done: %s, err: %v", task.Type(), err)
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

## 文件说明

| 文件 | 说明 |
| ------ | ------ |
| `config.go` | 配置结构与默认值填充 |
| `logger.go` | 项目 `logger` → `asynq.Logger` 适配 |
| `task.go` | Payload JSON 编解码辅助 |
| `client.go` | 生产者（`Producer`） |
| `server.go` | 消费者（`Consumer`） |
