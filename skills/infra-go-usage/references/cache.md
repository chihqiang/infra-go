# cache

统一缓存接口，提供**内存缓存**（`MemCache`）与 **Redis 分布式缓存**（`RedisCache`）两种实现，可在本地/分布式间按需切换。

## 特性

- **统一接口**：`Cache` 非泛型接口，内存与 Redis 实现一致，一个实例可存任意类型值（`any`）
- **过期删除**：支持默认/按 key 指定过期时间，过期时间带轻微抖动避免大量 key 同时过期（雪崩防护）
- **防缓存击穿**：`Take` 通过 SingleFlight 合并相同 key 的并发请求，底层只查一次
- **防缓存穿透**（Redis）：查询无结果时写入短时占位符，避免不存在 key 反复穿透 DB
- **LRU 淘汰**（内存）：`WithLimit` 限制容量，超出后淘汰最久未使用的 key
- **命中率统计**（内存）：周期性输出 QPS、命中率、元素数量（默认每分钟）
- **数值自增/自减**：`Increment` / `Decrement` 原子计数（内存加锁，Redis 用 INCRBY），key 不存在时自动初始化
- **按 key 设置存活时间**：`Expire` 为已有 key 设置 TTL，到期自动失效，无需重写整条缓存

## 安装

```bash
go get github.com/chihqiang/infra-go/cache
```

## 统一接口

未命中返回 `cache.ErrNotFound`，用标准库 `errors.Is(err, cache.ErrNotFound)` 判断（支持包装后的错误）：

| 方法 | 说明 |
| ------ | ------ |
| `Get(ctx, key) (any, error)` | 返回值；未命中或已过期返回 `ErrNotFound` |
| `Set(ctx, key, value any)` | 写入，使用默认过期时间 |
| `SetEx(ctx, key, value any, ttl)` | 写入并指定存活时间 `ttl`；`ttl <= 0` 回退默认 |
| `Delete(ctx, keys...)` | 删除一个或多个 key |
| `Take(ctx, key, fetch func()(any, error)) (any, error)` | 未命中时调用 `fetch` 获取并写入，并发去重防击穿 |
| `Increment(ctx, key, delta)` | 将 key 对应数值自增 `delta`；不存在时初始化为 `delta` |
| `Decrement(ctx, key, delta)` | 将 key 对应数值自减 `delta`；不存在时初始化为 `-delta` |
| `Expire(ctx, key, ttl)` | 为 key 设置存活时间 `ttl`，到期后自动失效；`ttl <= 0` 立即失效；key 不存在返回 `ErrNotFound` |

> 内存实现忽略 `ctx`；Redis 实现通过 `ctx` 传递超时与取消。
> 接口使用非泛型 `any`：调用方无需为每种值类型实例化缓存，取值后按需类型断言即可。

## 计数与过期

### 数值自增 / 自减

`Increment` / `Decrement` 用于计数器场景（访问量、库存、点赞等）。key 不存在时自动初始化为 `delta` / `-delta`；内存实现保持原值类型并加锁保证并发安全，Redis 实现走 `INCRBY` 原子操作：

```go
_ = c.Increment(ctx, "visit:20260828", 1) // 计数 +1，key 不存在则初始化为 1
_ = c.Decrement(ctx, "stock:sku1", 3)     // 库存 -3，key 不存在则初始化为 -3
```

> 注意：Redis 端自增后，`Get` 取回的是 `float64`（非泛型反序列化的既定行为），精确整数运算请直接用 `redisx.IncrBy` 的返回值。

### 设置存活时间（Expire）

为**已有 key** 设置 TTL，到期后自动失效，无需重写整条缓存（常用于续期、临时下架等场景）：

```go
_ = c.Set(ctx, "config:v1", cfg)            // 先写入
_ = c.Expire(ctx, "config:v1", time.Hour)   // 再设置 1 小时后失效
_ = c.Expire(ctx, "config:v1", 0)           // ttl <= 0：立即失效（等价于删除）
// key 不存在时返回 cache.ErrNotFound
```

> 精度差异：内存实现毫秒级、带抖动；Redis `EXPIRE` 为秒级精度，`Expire` 直接透传 `ttl` 不加抖动（避免被截断为 0 秒立即删除）。

## 内存缓存（MemCache）

```go
// ctx 由缓存实例持有，用于后台统计日志关联链路信息
c := cache.NewMemCache(ctx, time.Minute, cache.WithLimit(1000), cache.WithName("user"))
defer c.Close()

_ = c.Set(ctx, "key", "value")
v, err := c.Get(ctx, "key")
if err == nil {
    fmt.Println(v) // v 为 any，字符串可直接使用
}
_ = c.Delete(ctx, "key")
```

| 选项 | 说明 |
| ------ | ------ |
| `WithLimit(limit)` | 容量上限，超出按 LRU 淘汰；`<= 0` 表示不限制（默认） |
| `WithName(name)` | 缓存名称，用于统计日志标识 |

额外方法（仅 `MemCache`）：`Size()` 当前元素数量；`Close()` 停止后台统计 goroutine。

## Redis 缓存（RedisCache）

值以 JSON 序列化存储到 Redis；除通用特性外额外提供防缓存穿透与快速失败（Redis 故障时不穿透到 DB）。

```go
rds := redisx.MustNew(redisx.Config{Addr: cfg.RedisAddr})
c := cache.NewRedisCache(rds, cache.WithExpire(time.Minute))
defer rds.Close()

u, err := c.Take(ctx, "user:1", func() (any, error) {
    return loadUserFromDB(1) // 防击穿：并发下只执行一次
})
if err == nil {
    // 去泛型后取回的是 map[string]any，需要时用 json.Marshal/Unmarshal 还原为 *User
    var user *User
    _ = json.Unmarshal(mustMarshal(u), &user)
}
```

| 选项 | 说明 |
| ------ | ------ |
| `WithExpire(d)` | 默认过期时间；未设置默认 7 天 |
| `WithNotFoundExpire(d)` | 未命中占位符过期时间；未设置默认 1 分钟 |
| `WithCacheName(name)` | 缓存名称，用于日志标识 |

**防穿透示例**：`fetch` 返回 `cache.ErrNotFound` 时，会写入短时占位符，此后一段时间内相同的 `Take` 直接返回未命中而不再查询 DB。

## 防缓存击穿（Take）

多个协程同时 `Take` 同一个 key 时，底层 `fetch` 只执行一次，结果共享给所有调用者：

```go
val, err := c.Take(ctx, "user:123", func() (any, error) {
    return loadUserFromDB(123) // 并发场景下只执行一次
})
if err != nil {
    // 处理错误；fetch 失败不会写入缓存
}
```

## 关于类型（非泛型）

接口使用 `any` 存储值，调用方无需为每种类型单独实例化缓存，也不必定义泛型类型参数。取值后的处理：

- **内存缓存（MemCache）**：`Set` 存入的就是原对象，`Get/Take` 返回值类型与存入时一致，可直接断言，例如 `v.(*User)`。
- **Redis 缓存（RedisCache）**：值以 JSON 序列化存储，`Get` 时因无法得知目标类型，统一反序列化为 `map[string]any`（数字为 `float64`）；取回具体结构体时可用 `json.Marshal(got)` 后 `json.Unmarshal` 到目标类型，或自行按字段断言。
- 标量（string/int/bool 等）可直接使用返回值，无需额外处理。

> 若希望 Redis 缓存取回时直接得到具体类型，可考虑改用填充式 API（如 `Get(ctx, key, &user)`），本包暂未提供，可按需扩展。

## 注意事项

- `Take` 的 `fetch` 返回错误时不写入缓存，错误原样返回；Redis 实现中返回 `ErrNotFound` 时写入占位符
- 内存缓存仅**单进程内**共享；跨实例共享请用 `RedisCache`
- 过期时间实际在 `[0.95, 1.05] * expire` 内随机，避免同时过期
- `Expire`（Redis）直接透传 `ttl`（EXPIRE 秒级精度），不加抖动；内存实现带抖动
- 内存实现所有方法并发安全；`Close` 后实例不可再使用
