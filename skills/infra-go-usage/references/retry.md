# retry

重试工具包，支持指数退避、固定间隔、线性增长等延迟策略，支持自定义重试判定和回调。

## 特性

- **多种延迟策略**：指数退避（默认）、固定间隔、线性增长、自定义函数
- **随机抖动**：避免惊群效应
- **自定义重试判定**：RetryIf 函数决定哪些错误需要重试
- **重试回调**：每次重试前执行回调，方便日志记录
- **Context 支持**：支持超时和取消
- **最大延迟限制**：防止延迟过大
- **统一错误**：提供 `ErrMaxRetries`、`ErrNoRetry` 语义化错误

## 安装

```bash
go get github.com/chihqiang/infra-go/retry
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/retry"
)

func main() {
    var count int

    // 使用默认配置重试
    err := retry.Do(context.Background(), func(ctx context.Context) error {
        count++
        if count < 3 {
            return fmt.Errorf("temporary error (attempt %d)", count)
        }
        return nil
    })
    fmt.Println("result:", err, "attempts:", count)
}
```

## API

### 基本用法

```go
// 使用默认配置（3 次重试，100ms 初始延迟，指数退避）
err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})
```

### 自定义配置

```go
// 使用 Option 配置
err := retry.DoWithConfig(ctx, func(ctx context.Context) error {
    return callRemoteService()
}, 
    retry.WithMaxRetries(5),           // 最多重试 5 次
    retry.WithDelay(200*time.Millisecond), // 初始延迟 200ms
    retry.WithMaxDelay(5*time.Second),     // 最大延迟 5s
    retry.WithJitter(),                    // 启用随机抖动
    retry.WithOnRetry(func(attempt int, err error) {
        log.Printf("retry attempt %d: %v", attempt, err)
    }),
)
```

### 自定义重试判定

```go
// 仅对网络错误重试
err := retry.DoWithConfig(ctx, func(ctx context.Context) error {
    return callRemoteService()
},
    retry.WithMaxRetries(5),
    retry.WithRetryIf(func(err error) bool {
        var netErr net.Error
        return errors.As(err, &netErr) // 仅网络错误重试
    }),
)
```

### 延迟策略

```go
// 指数退避（默认）
retry.WithDelay(100*time.Millisecond)
// 延迟序列：100ms, 200ms, 400ms, 800ms...

// 固定间隔
retry.WithDelayFunc(retry.FixedDelay(500*time.Millisecond))
// 延迟序列：500ms, 500ms, 500ms...

// 线性增长
retry.WithDelayFunc(retry.LinearDelay(100*time.Millisecond, 100*time.Millisecond))
// 延迟序列：100ms, 200ms, 300ms, 400ms...

// 自定义指数退避
retry.WithDelayFunc(retry.ExponentialBackoff(10*time.Millisecond, 3))
// 延迟序列：10ms, 30ms, 90ms, 270ms...
```

### Context 超时

```go
// 设置总体超时
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})
```

### 重试回调

```go
err := retry.DoWithConfig(ctx, func(ctx context.Context) error {
    return callRemoteService()
},
    retry.WithMaxRetries(5),
    retry.WithOnRetry(func(attempt int, err error) {
        logger.Warn("retrying",
            zap.Int("attempt", attempt),
            zap.Error(err),
        )
    }),
)
```

## 配置选项

| 选项 | 说明 | 默认值 |
| ------ | ------ | -------- |
| `WithMaxRetries(n)` | 最大重试次数 | 3 |
| `WithDelay(d)` | 初始延迟 | 100ms |
| `WithMaxDelay(d)` | 最大延迟 | 10s |
| `WithDelayFunc(fn)` | 自定义延迟函数 | 指数退避 |
| `WithRetryIf(fn)` | 重试判定函数 | 所有 error 都重试 |
| `WithOnRetry(fn)` | 重试回调 | 无 |
| `WithJitter()` | 启用随机抖动 | false |

### 显式零值：不重试 / 重试不等待

`Config` 结构体采用「字段 == 0 视为未设置」的规则，因此**无法用字段表达 0**。
需要这些语义时把 Option 传给 `DoWithRetryConfig`（Option 在默认值填充之后应用）：

```go
// 不重试，只执行一次（Config{MaxRetries: 0} 会被填成默认 3 次）
err := retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithMaxRetries(0))

// 立即重试，不等待默认的 100ms 初始延迟
err = retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithDelay(0))

// 不限制延迟上限（Config{MaxDelay: 0} 会被填成默认 10s）
err = retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithMaxDelay(0))
```

> `Attempts(c, opts...)` 接受同一组 opts，用于预先得知总执行次数；
> `retry.Attempts(c, retry.WithMaxRetries(0))` 返回 `1`。
> `WithMaxDelay(0)` 的语义是「不限制上限」，而不是「把延迟截断为 0」。

## 错误处理

```go
err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})

switch {
case err == nil:
    // 成功
case retry.IsMaxRetries(err):
    // 超过最大重试次数
    log.Println("max retries exceeded:", err)
case retry.IsNoRetry(err):
    // 不再重试（RetryIf 返回 false）
    log.Println("no retry:", err)
default:
    // context 取消等
    log.Println("error:", err)
}
```

| 错误 | 说明 |
| ------ | ------ |
| `ErrMaxRetries` | 超过最大重试次数 |
| `ErrNoRetry` | 不再重试（RetryIf 返回 false） |
