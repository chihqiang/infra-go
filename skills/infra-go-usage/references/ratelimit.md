# ratelimit

限流器实现包，提供内存与 Redis 两种存储后端：令牌桶、滑动窗口、并发数限制与组合限流，可自由切换。

> **HTTP 限流中间件**不在本包提供：已统一收敛到 `httpx/middleware` 子包（`middleware.NewRateLimit`），并由 httpx 主包 `httpx.WithRateLimit` 便捷注册。本包只负责提供各类 `Limiter`。

## 特性

- **双存储后端**：内存（单机）与 Redis（分布式），经统一 `Limiter` 接口自由切换
- **令牌桶**：支持突发流量，以固定速率生成令牌
- **滑动窗口**：精确控制时间窗口内的请求数
- **并发数限制**：限制同时处理的请求数量（需手动 `Release`）
- **组合限流**：多个限流器链式组合，可混合内存与 Redis
- **Lua 脚本**：Redis 限流器用 Lua 脚本保证原子性
- **线程安全**：所有限流器均线程安全

## 安装

```bash
go get github.com/chihqiang/infra-go/ratelimit
```

## 限流器

### 令牌桶

以固定速率生成令牌，请求消耗令牌，支持突发流量：

```go
// --- 内存 ---
tb := ratelimit.NewTokenBucket(100, 200) // 100 QPS, burst 200

// --- Redis（多实例共享）---
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
tb := ratelimit.NewRedisTokenBucket(rdb, "rate:tb", 100, 200)

// 使用
if tb.Allow() { /* 请求通过 */ }

// 带 context（Redis 版本支持超时/取消）
ok, err := tb.AllowContext(ctx)
```

### 滑动窗口

在指定时间窗口内最多允许 N 次请求：

```go
sw := ratelimit.NewSlidingWindow(100, time.Second)                    // 内存：每秒 100 次
sw := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 100, time.Second) // Redis

if sw.Allow() { /* 请求通过 */ }
```

### 并发数限制

限制同时处理的请求数量，需手动 `Release`：

```go
c := ratelimit.NewConcurrency(10) // 最多 10 并发

if c.Allow() {
    defer c.Release()
    // 处理请求
}
```

### 组合限流

多个限流器链式组合（全部通过才放行），可混合内存与 Redis：

```go
memTB := ratelimit.NewTokenBucket(100, 200)
redisSW := ratelimit.NewRedisSlidingWindow(rdb, "rate:sw", 1000, time.Minute)

chain := ratelimit.NewChain(memTB, redisSW)
if chain.Allow() { /* 两个限流器都通过 */ }
```

## HTTP 限流中间件

HTTP 限流中间件已迁至 `httpx/middleware` 子包：`middleware.NewRateLimit(limiter, skipPaths...)`（面向对象，返回标准 `func(http.Handler) http.Handler`，可被 gin/echo 等复用）；httpx 主包内置 `httpx.WithRateLimit` 便捷注册。`limiter` 参数类型 `middleware.RateLimiter` 与本包 `Limiter` 方法集一致，本包各类限流器可直接传入。

特性：

- 被限流返回 **429 Too Many Requests**
- 通过 `AllowContext` 复用请求 context，Redis 限流器自动获得超时控制
- 限流组件异常时 **fail-open 放行**并记录错误日志，避免 Redis 抖动拖垮服务
- `skipPaths` 可跳过健康检查等路径（精确匹配或 `*` 前缀通配，同 `httpx.WithLogger`）
- `limiter` 为 nil 时降级为不限流并记录告警（不 panic）

### httpx 用法

```go
server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))          // 100 QPS、突发 200
server.Use(httpx.WithRateLimit(ratelimit.NewSlidingWindow(10, time.Minute))) // 每分钟 10 次
server.Use(httpx.WithRateLimit(redisLimiter, "/healthz", "/metrics"))        // 跳过探活接口
```

### 标准 net/http / 其它框架用法

```go
import "github.com/chihqiang/infra-go/httpx/middleware"

limiter := ratelimit.NewRedisTokenBucket(rdb, "api:limit", 100, 200)
handler := middleware.NewRateLimit(limiter).Middleware()(http.HandlerFunc(apiHandler))
http.ListenAndServe(":8080", handler)
```

### 按用户/IP 精细化限流

`httpx.WithRateLimit` 为全局限流（整个服务共享同一实例）。需要按用户、IP、路由等维度独立计数时，按维度 key 构建限流器并自行封装：

```go
func RateLimitByIP(rdb *redis.Client, rate, burst float64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip, _, err := net.SplitHostPort(r.RemoteAddr)
            if err != nil {
                ip = r.RemoteAddr
            }
            key := fmt.Sprintf("rate_limit:ip:%s", ip)
            limiter := ratelimit.NewRedisTokenBucket(rdb, key, rate, burst)
            if !limiter.Allow() {
                w.WriteHeader(http.StatusTooManyRequests)
                _, _ = w.Write([]byte(`{"code":429,"msg":"too many requests"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## 接口

所有限流器实现 `Limiter`，内存与 Redis 版本可自由替换：

```go
type Limiter interface {
    Allow() bool
    AllowContext(ctx context.Context) (bool, error)
}
```

## 工厂函数

通过 `StoreType` 自由切换存储后端：

```go
limiter := ratelimit.NewTokenBucketWithStore(
    ratelimit.StoreRedis,            // 或 ratelimit.StoreMemory
    rdb,                              // Redis 客户端（Memory 传 nil）
    "rate_limit_key",                 // 限流键名（Memory 传空）
    ratelimit.TokenBucketConfig{Rate: 100, Burst: 200},
)

limiter = ratelimit.NewSlidingWindowWithStore(
    ratelimit.StoreRedis,
    rdb,
    "rate_limit_key",
    ratelimit.SlidingWindowConfig{Limit: 100, Window: time.Second},
)
```

## 分布式场景

Redis 限流器适用于多实例部署，各实例共享同一 Redis 键实现全局限流：

```go
// 实例 1 与实例 2 共享同一个键 → 共享 100 QPS 配额
tb1 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)
tb2 := ratelimit.NewRedisTokenBucket(rdb, "shared:api:limit", 100, 200)
```

## 原理说明

### Redis 令牌桶

用 Lua 脚本 + Redis Hash（`tokens` / `last_update`）保证原子性：读取令牌数与上次更新时间 → 计算期间新增令牌 → 判断是否足够并消耗 → 更新状态与过期时间。

### Redis 滑动窗口

用 Lua 脚本 + Redis ZSET 实现：`ZREMRANGEBYSCORE` 移除窗口外旧记录 → `ZCARD` 统计当前窗口请求数 → 未超限则 `ZADD` 当前时间戳 → 设置键过期自动清理。

## 错误处理

```go
if !limiter.Allow() { /* 被限流 */ }

ok, err := limiter.AllowContext(ctx)
if err != nil { /* Redis 错误或 context 取消 */ }
if !ok { /* 被限流 */ }
```

| 错误 | 说明 |
|------|------|
| `ErrLimitExceeded` | 超出限流 |
