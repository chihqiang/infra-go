# breaker

熔断器，基于 Google SRE 自适应过载算法，保护下游依赖（数据库、HTTP API、Redis 等）在故障时不被级联拖垮。

## 特性

- **Google SRE 算法**：滑动窗口统计错误率，按概率拒绝请求，避免雪崩
- **三态自动流转**：关闭 → 打开 → 半开（冷却期后放行探测请求，成功则恢复）
- **快速失败**：熔断打开时请求立即返回 `ErrServiceUnavailable`，不再等待慢速下游
- **自定义错误策略**：`DoWithAcceptable` 排除 4xx 等业务错误，精确感知下游健康
- **降级支持**：`DoWithFallback` 熔断打开时执行兜底逻辑（缓存、队列、友好错误）
- **Promise 模式**：`Allow` 手动控制成功/失败上报
- **全局管理**：按名称共享熔断器，`Do` 系列便捷函数开箱即用

## 安装

```bash
go get github.com/chihqiang/infra-go/breaker
```

## 基本用法

```go
b := breaker.NewBreaker(breaker.WithName("payment-gateway"))

// 简单模式：统计所有非 nil 错误
err := b.Do(func() error {
    return callPaymentAPI(req)
})
if errors.Is(err, breaker.ErrServiceUnavailable) {
    // 熔断器打开，下游不可用
}
```

## 降级（DoWithFallback）

熔断打开时执行降级逻辑：

```go
err := b.DoWithFallback(func() error {
    return callPaymentAPI(req)
}, func(err error) error {
    // 返回缓存结果、加入重试队列，或返回友好错误
    return serveCachedResult(req)
})
```

## 排除业务错误（DoWithAcceptable）

精确控制哪些错误计入失败计数，避免 `ErrNotFound` 等业务错误触发熔断：

```go
err := b.DoWithAcceptable(func() error {
    return callPaymentAPI(req)
}, func(err error) bool {
    // 返回 true = "此错误可接受，不计入熔断计数"
    return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized)
})
```

组合使用 `DoWithFallbackAcceptable` 可同时降级 + 自定义错误策略。

## Promise 模式（手动控制）

`Allow` 适合需要异步上报结果的场景：

```go
promise, err := b.Allow()
if err != nil {
    // 熔断器打开
    return err
}
// 执行请求后上报结果
if success {
    promise.Accept()
} else {
    promise.Reject("upstream timeout")
}
```

## 全局管理器

按名称共享熔断器，适合多处调用同一下游的场景（统计一致）：

```go
// 同名的熔断器全局共享
err := breaker.Do("payment-gateway", func() error {
    return callPaymentAPI(req)
})

// 关闭某条调用链的熔断保护
breaker.NoBreakerFor("health-check")
```

## 上下文版本

所有 `Do` 方法都有 `DoCtx` 变体，context 取消时直接返回 context 错误，不执行请求：

```go
err := b.DoCtx(ctx, func() error {
    return callPaymentAPI(req)
})
```

## 方法一览

| 方法 | 说明 |
| ------ | ------ |
| `Allow()` / `AllowCtx(ctx)` | 检查是否放行，返回 Promise |
| `Do(req)` / `DoCtx(ctx, req)` | 执行请求，熔断打开立即失败 |
| `DoWithAcceptable(req, acceptable)` | 自定义错误可接受策略 |
| `DoWithFallback(req, fallback)` | 熔断打开时执行降级 |
| `DoWithFallbackAcceptable(req, fallback, acceptable)` | 降级 + 自定义错误策略 |
| `GetBreaker(name)` / `Do(name, req)` | 全局共享熔断器 |

## 注意事项

- **不要吞掉错误**：熔断器依赖准确的错误反馈计算错误率，不要在回调中吞掉真实错误
- **配合超时使用**：熔断器防止级联失败，但仍需为每次调用设置超时，避免 goroutine 堆积
- **为熔断器命名**：唯一名称使日志能区分不同故障来源
- 熔断打开时会输出告警日志，包含最近 5 条失败原因，便于排查
