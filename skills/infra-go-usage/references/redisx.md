# redisx

A Redis client wrapper built on [go-redis/v9](https://github.com/redis/go-redis), providing
connection pool management, key prefixes, distributed locks, and more.

## Features

- **Connection pool management**: configurable pool size, timeouts, and more
- **Sentinel mode**: supports Redis Sentinel for high availability
- **Key prefix**: every operation gets the prefix automatically, so several services can share
  one Redis instance easily
- **Distributed lock**: built on SET NX EX + Lua scripts, with auto-renewal; when the supplied
  context is cancelled the renewal goroutine stops automatically, preventing leaks
- **Complete API**: covers the common String, Hash, List, and Set operations
- **Config-driven**: Config defines defaults through `default` struct tags, following the conf standard
- **Unified errors**: semantic errors (`ErrNil`, `ErrLockNotAcquired`, etc.)

## Installation

```bash
go get github.com/chihqiang/infra-go/redisx
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/redisx"
)

func main() {
    // initialize the client
    client := redisx.MustNew(redisx.Config{
        Addr:      "127.0.0.1:6379",
        Password:  "secret",
        DB:        0,
        KeyPrefix: "myapp",
    })
    defer client.Close()

    ctx := context.Background()

    // basic operations
    client.Set(ctx, "hello", "world", 10*time.Minute)
    val, _ := client.Get(ctx, "hello")
    fmt.Println(val) // Output: world (the key actually stored is myapp:hello)

    // distributed lock
    lock, err := client.Locker("order:123", 10*time.Second).TryLock(ctx)
    if err != nil {
        fmt.Println("lock not acquired")
        return
    }
    defer lock.Unlock(ctx)

    // run the business logic
    fmt.Println("doing work with lock held")
}
```

## API

### Creating a client

```go
// create a client (returns an error)
client, err := redisx.New(redisx.Config{
    Addr:     "127.0.0.1:6379",
    Password: "secret",
})

// create a client (panics on error, suited to global initialization)
client := redisx.MustNew(redisx.Config{Addr: "127.0.0.1:6379"})
```

### Basic operations

```go
// String operations
client.Set(ctx, "key", "value", 10*time.Minute)
val, err := client.Get(ctx, "key")
n, _ := client.Incr(ctx, "counter")
n, _ := client.IncrBy(ctx, "counter", 5)

// Generic operations
n, _ := client.Del(ctx, "key1", "key2")
exists, _ := client.Exists(ctx, "key")
ok, _ := client.Expire(ctx, "key", 5*time.Minute)
ttl, _ := client.TTL(ctx, "key")
```

### Hash operations

```go
client.HSet(ctx, "user:1", "name", "alice", "age", 30)
name, _ := client.HGet(ctx, "user:1", "name")
all, _ := client.HGetAll(ctx, "user:1")
n, _ := client.HDel(ctx, "user:1", "age")
```

### List operations

```go
client.LPush(ctx, "queue", "task1", "task2")
client.RPush(ctx, "queue", "task3")
val, _ := client.LPop(ctx, "queue")
items, _ := client.LRange(ctx, "queue", 0, -1)
```

### Set operations

```go
client.SAdd(ctx, "tags", "go", "redis")
members, _ := client.SMembers(ctx, "tags")
ok, _ := client.SIsMember(ctx, "tags", "go")
n, _ := client.SRem(ctx, "tags", "go")
```

### Scan operations

```go
// iterate keys; the prefix is handled automatically
keys, cursor, err := client.Scan(ctx, 0, "user:*", 100)
```

### Distributed lock

```go
// Try to acquire the lock (non-blocking)
// The ctx passed in controls the lifetime of the auto-renewal goroutine:
// when ctx is cancelled, the renewal goroutine stops automatically, preventing leaks
lock, err := client.Locker("resource:1", 10*time.Second).TryLock(ctx)
if err != nil {
    // lock already held
    return
}
defer lock.Unlock(ctx)

// Acquire the lock in blocking mode (retries automatically)
lock, err = client.Locker("resource:1", 10*time.Second).Lock(ctx, 500*time.Millisecond)
defer lock.Unlock(ctx)

// Auto-renewal (keeps the business logic from outliving the lock TTL)
// Enable it by appending options to Locker, for example
//     client.Locker(key, 0, redisx.WithTTL(10*time.Second), redisx.WithAutoRenew())
// There is no separate LockerWithTTL API; the TTL comes from Locker's 2nd arg or WithTTL
// The renewal goroutine watches ctx.Done(), so nothing leaks even if the caller forgets Unlock
```

> **TTL must be greater than 0**: when the `Locker` TTL (or the value after `WithTTL` overrides
> it) is below `1ms`, `TryLock`/`Lock` return `redisx.ErrInvalidLockTTL` immediately without
> writing to Redis. This is because a TTL <= 0 counts as **never expiring** in Redis (a crashed
> holder means a permanent deadlock), and enabling `WithAutoRenew` would trigger a
> `time.NewTicker(0)` panic inside the renewal goroutine (which cannot be recovered and kills the
> process); a TTL < 1ms would be truncated to 0 by the millisecond rounding in the renewal
> script, running `PEXPIRE key 0` and deleting the lock instantly.

### Convenience method

```go
// Run a function under a distributed lock; the lock is released when it returns
err := client.SetNXWithLock(ctx, "task:1", 30*time.Second, func(ctx context.Context) error {
    // run with the lock held
    return doWork(ctx)
})
```

## Configuration

### Configuration fields

| Field | Type | Default | Description |
| ------ | ------ | -------- | ------ |
| `Addr` | `string` | `127.0.0.1:6379` | Redis server address |
| `Username` | `string` | `""` | Username (Redis 6.0+ ACL) |
| `Password` | `string` | `""` | Password |
| `DB` | `int` | `0` | Database number |
| `MasterName` | `string` | `""` | Sentinel master name |
| `SentinelAddrs` | `[]string` | `nil` | List of sentinel addresses |
| `PoolSize` | `int` | `10` | Connection pool size |
| `MinIdleConns` | `int` | `2` | Minimum number of idle connections |
| `MaxRetries` | `int` | `3` | Maximum number of command retries |
| `DialTimeout` | `time.Duration` | `5s` | Dial timeout |
| `ReadTimeout` | `time.Duration` | `3s` | Read timeout |
| `WriteTimeout` | `time.Duration` | `3s` | Write timeout |
| `PoolTimeout` | `time.Duration` | `4s` | Timeout for taking a connection from the pool |
| `ConnMaxIdleTime` | `time.Duration` | `5m` | Maximum idle time for a connection |
| `KeyPrefix` | `string` | `""` | Key prefix |

## Error handling

```go
val, err := client.Get(ctx, "key")
switch {
case err == nil:
    // success
case errors.Is(err, redisx.ErrNil):
    // key does not exist
default:
    // other errors
}

lock, err := client.Locker("key", 10*time.Second).TryLock(ctx)
if redisx.IsLockNotAcquired(err) {
    // lock already held
}
```

| Error | Description |
| ------ | ------ |
| `ErrNil` | Key does not exist |
| `ErrLockNotAcquired` | Failed to acquire the lock (already held) |
| `ErrLockOwnershipMismatch` | Failed to release the lock (not owned by the current holder) |
