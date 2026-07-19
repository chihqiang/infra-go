# syncx

并发工具包，提供常用的并发控制原语。

## 特性

- **SingleFlight**：防止缓存击穿，相同 key 的并发调用只执行一次
- **ConcurrentMap**：泛型分段锁 Map，高并发读写性能优于 `sync.Map`
- **Semaphore**：信号量，控制并发数量
- **OnceValue / OnceError**：泛型懒加载，只执行一次
- **OrDone / Merge / FanOut**：Channel 流水线工具

## 安装

```bash
go get github.com/chihqiang/infra-go/syncx
```

## SingleFlight

防止缓存击穿，相同 key 的并发调用只执行一次，结果共享给所有调用者。

```go
sf := syncx.NewSingleFlight[string]()

// 100 个协程同时查询同一个 key，只穿透到底层数据源一次
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        val, err := sf.Do("user:123", func() (string, error) {
            return fetchUserFromDB(123) // 只执行一次
        })
        // 所有协程拿到相同的结果
    }()
}
wg.Wait()
```

支持 context 取消：

```go
val, err := sf.DoCtx(ctx, "user:123", func(ctx context.Context) (string, error) {
    return fetchUserWithTimeout(ctx, 123)
})
```

| 方法 | 说明 |
| ------ | ------ |
| `Do(key, fn)` | 执行 fn，相同 key 并发只执行一次 |
| `DoCtx(ctx, key, fn)` | 同上，支持 context 取消 |
| `Forget(key)` | 清除 key 的调用记录，下次重新执行 |

## ConcurrentMap

泛型分段锁 Map，默认 32 个分段，比全局单一锁有更好的并发性能。

```go
m := syncx.NewConcurrentMap[string, int]()

m.Set("a", 1)
val, ok := m.Get("a")       // 1, true
val, ok = m.GetOrSet("b", 2) // 2, false (新设置)
m.Delete("a")

// 遍历
m.Range(func(key string, value int) bool {
    fmt.Printf("%s=%d\n", key, value)
    return true // 返回 false 停止遍历
})

// 获取所有键/值
keys := m.Keys()
values := m.Values()

m.Clear()
```

| 方法 | 说明 |
| ------ | ------ |
| `Set(key, value)` | 设置键值对 |
| `Get(key)` | 获取值，返回 (value, ok) |
| `GetOrSet(key, default)` | 获取或设置默认值 |
| `GetAndDelete(key)` | 获取并删除 |
| `Delete(key)` | 删除键 |
| `Has(key)` | 检查键是否存在 |
| `Len()` | 返回键值对数量 |
| `Range(fn)` | 遍历所有键值对 |
| `Keys()` | 返回所有键 |
| `Values()` | 返回所有值 |
| `Clear()` | 清空所有键值对 |

## Semaphore

信号量，用于控制并发数量。

```go
sem := syncx.NewSemaphore(10) // 最多 10 个并发

for _, task := range tasks {
    sem.Acquire()
    go func(t Task) {
        defer sem.Release()
        doWork(t)
    }(task)
}
sem.Wait() // 等待所有完成
```

非阻塞尝试获取：

```go
if sem.TryAcquire() {
    defer sem.Release()
    doWork()
} else {
    // 信号量已满，跳过或排队
}
```

| 方法 | 说明 |
| ------ | ------ |
| `Acquire()` | 获取信号量，满则阻塞 |
| `TryAcquire()` | 尝试获取，满则返回 false |
| `Release()` | 释放信号量 |
| `Wait()` | 等待所有已获取的信号量释放 |
| `Capacity()` | 返回最大并发数 |
| `Available()` | 返回当前可用数量 |

## OnceValue / OnceError

泛型懒加载，保证函数只执行一次。

```go
// 不带 error
var config = syncx.NewOnceValue(func() *Config {
    return loadConfig() // 只加载一次
})
cfg := config.Get()

// 带 error
var conn = syncx.NewOnceError(func() (*sql.DB, error) {
    return sql.Open("mysql", dsn) // 只连接一次
})
db, err := conn.Get()
```

## Channel 工具

### OrDone

当 context 取消或源 channel 关闭时关闭输出 channel：

```go
for v := range syncx.OrDoneCtx(ctx, src) {
    process(v) // ctx 取消时自动停止
}
```

### Merge

将多个 channel 合并为一个：

```go
merged := syncx.Merge(ctx, ch1, ch2, ch3)
for v := range merged {
    process(v)
}
```

### FanOut

将输入 channel 的值分发到多个输出 channel：

```go
outs := syncx.FanOut(ctx, input, 3) // 3 个输出 channel
for _, out := range outs {
    go func(ch <-chan T) {
        for v := range ch {
            process(v)
        }
    }(out)
}
```

## 目录结构

```text
syncx/
├── singleflight.go     — SingleFlight 防缓存击穿
├── concurrent_map.go   — 泛型分段锁 ConcurrentMap
├── semaphore.go        — 信号量
├── once.go             — OnceValue / OnceError 懒加载
├── ordone.go           — OrDone / Merge / FanOut channel 工具
├── atomic.go           — 内部 hash 辅助函数
├── syncx_test.go       — 单元测试
└── README.md
```
