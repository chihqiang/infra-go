# redisx

基于 [go-redis/v9](https://github.com/redis/go-redis) 的 Redis 客户端封装，提供连接池管理、键名前缀、分布式锁等功能。

## 特性

- **连接池管理**：可配置连接池大小、超时时间等
- **哨兵模式**：支持 Redis Sentinel 高可用
- **键名前缀**：所有操作自动添加前缀，方便多服务共享 Redis
- **分布式锁**：基于 SET NX EX + Lua 脚本实现，支持自动续期
- **完整 API**：覆盖 String、Hash、List、Set 等常用操作
- **配置驱动**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **统一错误**：提供语义化错误（`ErrNil`、`ErrLockNotAcquired` 等）

## 安装

```bash
go get github.com/chihqiang/infra-go/redisx
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/redisx"
)

func main() {
    // 初始化客户端
    client := redisx.MustNew(redisx.Config{
        Addr:      "127.0.0.1:6379",
        Password:  "secret",
        DB:        0,
        KeyPrefix: "myapp",
    })
    defer client.Close()

    ctx := context.Background()

    // 基础操作
    client.Set(ctx, "hello", "world", 10*time.Minute)
    val, _ := client.Get(ctx, "hello")
    fmt.Println(val) // 输出: world（实际存储的键为 myapp:hello）

    // 分布式锁
    lock, err := client.Locker("order:123", 10*time.Second).TryLock(ctx)
    if err != nil {
        fmt.Println("lock not acquired")
        return
    }
    defer lock.Unlock(ctx)

    // 执行业务逻辑
    fmt.Println("doing work with lock held")
}
```

## API

### 创建客户端

```go
// 创建客户端（返回 error）
client, err := redisx.New(redisx.Config{
    Addr:     "127.0.0.1:6379",
    Password: "secret",
})

// 创建客户端（出错 panic，适合全局初始化）
client := redisx.MustNew(redisx.Config{Addr: "127.0.0.1:6379"})
```

### 基础操作

```go
// String 操作
client.Set(ctx, "key", "value", 10*time.Minute)
val, err := client.Get(ctx, "key")
n, _ := client.Incr(ctx, "counter")
n, _ := client.IncrBy(ctx, "counter", 5)

// 通用操作
n, _ := client.Del(ctx, "key1", "key2")
exists, _ := client.Exists(ctx, "key")
ok, _ := client.Expire(ctx, "key", 5*time.Minute)
ttl, _ := client.TTL(ctx, "key")
```

### Hash 操作

```go
client.HSet(ctx, "user:1", "name", "alice", "age", 30)
name, _ := client.HGet(ctx, "user:1", "name")
all, _ := client.HGetAll(ctx, "user:1")
n, _ := client.HDel(ctx, "user:1", "age")
```

### List 操作

```go
client.LPush(ctx, "queue", "task1", "task2")
client.RPush(ctx, "queue", "task3")
val, _ := client.LPop(ctx, "queue")
items, _ := client.LRange(ctx, "queue", 0, -1)
```

### Set 操作

```go
client.SAdd(ctx, "tags", "go", "redis")
members, _ := client.SMembers(ctx, "tags")
ok, _ := client.SIsMember(ctx, "tags", "go")
n, _ := client.SRem(ctx, "tags", "go")
```

### Scan 操作

```go
// 迭代键，自动处理前缀
keys, cursor, err := client.Scan(ctx, 0, "user:*", 100)
```

### 分布式锁

```go
// 尝试获取锁（非阻塞）
lock, err := client.Locker("resource:1", 10*time.Second).TryLock(ctx)
if err != nil {
    // 锁已被持有
    return
}
defer lock.Unlock(ctx)

// 阻塞式获取锁（自动重试）
lock, err = client.Locker("resource:1", 10*time.Second).Lock(ctx, 500*time.Millisecond)
defer lock.Unlock(ctx)

// 自动续期（防止业务执行时间超过锁过期时间）
// 需要单独使用 LockerWithTTL 和 WithAutoRenew
```

### 便捷方法

```go
// 使用分布式锁执行函数，执行完自动释放锁
err := client.SetNXWithLock(ctx, "task:1", 30*time.Second, func(ctx context.Context) error {
    // 在锁保护下执行
    return doWork(ctx)
})
```

## 配置

### 配置项说明

| 字段 | 类型 | 默认值 | 说明 |
| ------ | ------ | -------- | ------ |
| `Addr` | `string` | `127.0.0.1:6379` | Redis 服务器地址 |
| `Username` | `string` | `""` | 用户名（Redis 6.0+ ACL） |
| `Password` | `string` | `""` | 密码 |
| `DB` | `int` | `0` | 数据库编号 |
| `MasterName` | `string` | `""` | 哨兵主节点名称 |
| `SentinelAddrs` | `[]string` | `nil` | 哨兵节点地址列表 |
| `PoolSize` | `int` | `10` | 连接池大小 |
| `MinIdleConns` | `int` | `2` | 最小空闲连接数 |
| `MaxRetries` | `int` | `3` | 命令最大重试次数 |
| `DialTimeout` | `time.Duration` | `5s` | 连接超时 |
| `ReadTimeout` | `time.Duration` | `3s` | 读取超时 |
| `WriteTimeout` | `time.Duration` | `3s` | 写入超时 |
| `PoolTimeout` | `time.Duration` | `4s` | 连接池获取超时 |
| `IdleConnTimeout` | `time.Duration` | `5m` | 空闲连接超时 |
| `KeyPrefix` | `string` | `""` | 键名前缀 |

## 错误处理

```go
val, err := client.Get(ctx, "key")
switch {
case err == nil:
    // 成功
case errors.Is(err, redisx.ErrNil):
    // 键不存在
default:
    // 其他错误
}

lock, err := client.Locker("key", 10*time.Second).TryLock(ctx)
if redisx.IsLockNotAcquired(err) {
    // 锁已被持有
}
```

| 错误 | 说明 |
| ------ | ------ |
| `ErrNil` | 键不存在 |
| `ErrLockNotAcquired` | 获取锁失败（锁已被持有） |
| `ErrLockOwnershipMismatch` | 释放锁失败（锁不属于当前持有者） |
