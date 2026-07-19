# ratelimit

限流工具包，提供内存和 Redis 两种存储后端，支持令牌桶、滑动窗口、并发数限制和组合限流，可自由切换存储方式。

## 特性

- **双存储后端**：内存（单机）和 Redis（分布式），通过统一接口自由切换
- **令牌桶**：支持突发流量，以固定速率生成令牌
- **滑动窗口**：精确控制时间窗口内的请求数
- **并发数限制**：限制同时处理的请求数量
- **组合限流**：多个限流器链式组合，可混合内存和 Redis
- **Lua 脚本**：Redis 限流器使用 Lua 脚本保证原子性
- **线程安全**：所有限流器均线程安全

## 安装

```bash
go get github.com/chihqiang/infra-go/ratelimit
```

## 快速开始

### 内存限流（单机）

```go
package main

import (
    "fmt"

    "github.com/chihqiang/infra-go/ratelimit"
)

func main() {
    // 内存令牌桶：100 QPS，突发 200
    limiter := ratelimit.NewTokenBucket(100, 200)

    if limiter.Allow() {
        fmt.Println("allowed")
    } else {
        fmt.Println("rate limit exceeded")
    }
}
```

### Redis 限流（分布式）

```go
package main

import (
    "context"
    "fmt"

    "github.com/chihqiang/infra-go/ratelimit"
    "github.com/redis/go-redis/v9"
)

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})

    // Redis 令牌桶：100 QPS，突发 200，多实例共享限流
    limiter := ratelimit.NewRedisTokenBucket(rdb, "api:rate_limit", 100, 200)

    ok, err := limiter.AllowContext(context.Background())
    if err != nil {
        fmt.Println("error:", err)
        return
    }
    if ok {
        fmt.Println("allowed")
    } else {
        fmt.Println("rate limit exceeded")
    }
}
```

### 工厂函数（自由切换）

```go
// 通过配置切换存储后端
var limiter ratelimit.Limiter

if config.UseRedis {
    limiter = ratelimit.NewTokenBucketWithStore(
        ratelimit.StoreRedis, rdb, "api:rate_limit",
        ratelimit.TokenBucketConfig{Rate: 100, Burst: 200},
    )
} else {
    limiter = ratelimit.NewTokenBucketWithStore(
        ratelimit.StoreMemory, nil, "",
        ratelimit.TokenBucketConfig{Rate: 100, Burst: 200},
    )
}
```

## 限流器

### 令牌桶

以固定速率生成令牌，请求消耗令牌，支持突发流量。

```go
// --- 内存 ---
tb := ratelimit.NewTokenBucket(100, 200) // 100 QPS, burst 200

// --- Redis ---
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
tb := ratelimit.NewRedisTokenBucket(rdb, "rate:tb", 100, 200)

// 使用
if tb.Allow() {
    // 请求通过
}

// 带 context（Redis 版本支持超时）
ok, err := tb.AllowContext(ctx)
```

### 滑动窗口

在指定时间窗口内最多允许 N 次请求。

```go
// --- 内存 ---
sw := ratelimit.NewSlidingWindow(100, time.Second) // 每秒 100 次

// --- Redis ---
sw := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 100, time.Second)

// 使用
if sw.Allow() {
    // 请求通过
}
```

### 并发数限制

限制同时处理的请求数量，需手动 Release。

```go
c := ratelimit.NewConcurrency(10) // 最多 10 并发

if c.Allow() {
    defer c.Release()
    // 处理请求
}
```

### 组合限流

多个限流器链式组合，可混合内存和 Redis。

```go
// 内存令牌桶 + Redis 滑动窗口
memTB := ratelimit.NewTokenBucket(100, 200)
redisSW := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 1000, time.Minute)

chain := ratelimit.NewChain(memTB, redisSW)

if chain.Allow() {
    // 两个限流器都通过
}
```

## HTTP 中间件示例

本包不内置 HTTP 中间件，但可以很方便地自行封装。以下是几种常见写法：

### 基本中间件

```go
// RateLimitMiddleware 将限流器包装为 HTTP 中间件。
// 被限流时返回 429 状态码。
func RateLimitMiddleware(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !limiter.Allow() {
                w.WriteHeader(http.StatusTooManyRequests)
                w.Write([]byte(`{"code":429,"msg":"rate limit exceeded"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### 支持 context 超时（Redis 版本）

```go
func RateLimitMiddlewareWithContext(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 使用请求 context，支持超时和取消
            ok, err := limiter.AllowContext(r.Context())
            if err != nil {
                w.WriteHeader(http.StatusInternalServerError)
                w.Write([]byte(`{"code":500,"msg":"rate limiter error"}`))
                return
            }
            if !ok {
                w.WriteHeader(http.StatusTooManyRequests)
                w.Write([]byte(`{"code":429,"msg":"rate limit exceeded"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### 按用户/IP 限流

通过为每个用户或 IP 创建独立的限流键，实现精细化的限流：

```go
func RateLimitByIP(rdb *redis.Client, rate float64, burst float64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 提取客户端 IP
            ip, _, err := net.SplitHostPort(r.RemoteAddr)
            if err != nil {
                ip = r.RemoteAddr
            }

            key := fmt.Sprintf("rate_limit:ip:%s", ip)
            limiter := ratelimit.NewRedisTokenBucket(rdb, key, rate, burst)

            if !limiter.Allow() {
                w.WriteHeader(http.StatusTooManyRequests)
                w.Write([]byte(`{"code":429,"msg":"too many requests"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### 使用示例

```go
func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
    limiter := ratelimit.NewRedisTokenBucket(rdb, "api:limit", 100, 200)

    mux := http.NewServeMux()
    mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("ok"))
    })

    // 应用限流中间件
    handler := RateLimitMiddleware(limiter)(mux)
    http.ListenAndServe(":8080", handler)
}
```

## 接口

所有限流器实现 `Limiter` 接口，内存和 Redis 版本可自由替换：

```go
type Limiter interface {
    Allow() bool
    AllowContext(ctx context.Context) (bool, error)
}
```

## 工厂函数

通过 `StoreType` 自由切换存储后端：

```go
// 令牌桶
limiter := ratelimit.NewTokenBucketWithStore(
    ratelimit.StoreRedis,   // 或 ratelimit.StoreMemory
    rdb,                     // Redis 客户端（Memory 时传 nil）
    "rate_limit_key",        // 限流键名（Memory 时传空）
    ratelimit.TokenBucketConfig{Rate: 100, Burst: 200},
)

// 滑动窗口
limiter := ratelimit.NewSlidingWindowWithStore(
    ratelimit.StoreRedis,
    rdb,
    "rate_limit_key",
    ratelimit.SlidingWindowConfig{Limit: 100, Window: time.Second},
)
```

## 分布式场景

Redis 限流器适用于多实例部署场景，所有实例共享同一个 Redis，实现全局限流：

```go
// 实例 1
tb1 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)

// 实例 2（共享同一个限流配额）
tb2 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)

// 两个实例共享 100 QPS 的限流配额
```

## 原理说明

### Redis 令牌桶

使用 Lua 脚本保证原子性，通过 Redis Hash 存储 `tokens` 和 `last_update`：

1. 读取当前令牌数和上次更新时间
2. 计算经过时间生成的新令牌
3. 判断是否有足够令牌，有则消耗并返回允许
4. 更新状态并设置过期时间

### Redis 滑动窗口

使用 Lua 脚本 + Redis ZSET（有序集合）实现：

1. 移除窗口外的旧记录（`ZREMRANGEBYSCORE`）
2. 统计当前窗口内请求数（`ZCARD`）
3. 未超限则添加当前请求时间戳（`ZADD`）
4. 设置键过期时间自动清理

## 错误处理

```go
if !limiter.Allow() {
    // 被限流
}

// context 版本
ok, err := limiter.AllowContext(ctx)
if err != nil {
    // Redis 操作错误或 context 取消
}
if !ok {
    // 被限流
}
```

| 错误 | 说明 |
| ------ | ------ |
| `ErrLimitExceeded` | 超出限流 |
