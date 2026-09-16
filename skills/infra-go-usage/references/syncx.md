# syncx

A concurrency utility package providing common concurrency control primitives.

## Features

- **SingleFlight**: cache stampede protection — concurrent calls with the same key run only once
- **ConcurrentMap**: a generic sharded lock map, outperforming `sync.Map` under heavy concurrent
  read/write
- **Semaphore**: controls the level of concurrency
- **OnceValue / OnceError**: generic lazy initialization that runs only once
- **OrDone / Merge / FanOut**: channel pipeline helpers

## Installation

```bash
go get github.com/chihqiang/infra-go/syncx
```

## SingleFlight

Cache stampede protection: concurrent calls with the same key run only once, and the result is
shared with every caller.

```go
sf := syncx.NewSingleFlight[string]()

// 100 goroutines query the same key at once; only one reaches the underlying data source
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        val, err := sf.Do("user:123", func() (string, error) {
            return fetchUserFromDB(123) // runs only once
        })
        // every goroutine gets the same result
    }()
}
wg.Wait()
```

Context cancellation is supported:

```go
val, err := sf.DoCtx(ctx, "user:123", func(ctx context.Context) (string, error) {
    return fetchUserWithTimeout(ctx, 123)
})
```

| Method | Description |
| ------ | ------ |
| `Do(key, fn)` | Run fn; concurrent calls with the same key run it only once |
| `DoCtx(ctx, key, fn)` | Same as above, with context cancellation |
| `Forget(key)` | Clear the key's call record so the next call runs fn again |

> **A panic propagates to every waiter**: if the leader's `fn` panics, the waiters receive
> **the same panic value and panic as well** instead of being handed a zero value. A caller's
> `recover` must therefore cover every `Do`/`DoCtx` call site, not just the one that started the
> call.
>
> This design avoids "silent data corruption": during a cache stampede, if waiters received a
> zero value plus a `nil` error, upper layers would mistake it for a successful load and write the
> zero value back to the cache or hand it to users.

## ConcurrentMap

A generic sharded lock map with 32 shards by default, giving better concurrency than a single
global lock.

```go
m := syncx.NewConcurrentMap[string, int]()

m.Set("a", 1)
val, ok := m.Get("a")       // 1, true
val, ok = m.GetOrSet("b", 2) // 2, false (newly set)
m.Delete("a")

// iterate
m.Range(func(key string, value int) bool {
    fmt.Printf("%s=%d\n", key, value)
    return true // return false to stop iterating
})

// get all keys/values
keys := m.Keys()
values := m.Values()

m.Clear()
```

| Method | Description |
| ------ | ------ |
| `Set(key, value)` | Set a key/value pair |
| `Get(key)` | Get a value, returning (value, ok) |
| `GetOrSet(key, default)` | Get the value, or set the default |
| `GetAndDelete(key)` | Get and delete |
| `Delete(key)` | Delete a key |
| `Has(key)` | Check whether a key exists |
| `Len()` | Number of key/value pairs |
| `Range(fn)` | Iterate over every key/value pair |
| `Keys()` | Return all keys |
| `Values()` | Return all values |
| `Clear()` | Remove all key/value pairs |

## Semaphore

A semaphore used to limit concurrency.

```go
sem := syncx.NewSemaphore(10) // at most 10 concurrent

for _, task := range tasks {
    sem.Acquire()
    go func(t Task) {
        defer sem.Release()
        doWork(t)
    }(task)
}
sem.Wait() // wait for all of them to finish
```

Non-blocking acquisition attempt:

```go
if sem.TryAcquire() {
    defer sem.Release()
    doWork()
} else {
    // the semaphore is full; skip or queue
}
```

| Method | Description |
| ------ | ------ |
| `Acquire()` | Acquire; blocks when full |
| `TryAcquire()` | Try to acquire; returns false when full |
| `Release()` | Release the semaphore |
| `Wait()` | Wait until every acquired slot is released |
| `Capacity()` | Return the maximum concurrency |
| `Available()` | Return the number currently available |

## OnceValue / OnceError

Generic lazy initialization that guarantees the function runs only once.

```go
// without an error
var config = syncx.NewOnceValue(func() *Config {
    return loadConfig() // loaded only once
})
cfg := config.Get()

// with an error
var conn = syncx.NewOnceError(func() (*sql.DB, error) {
    return sql.Open("mysql", dsn) // connected only once
})
db, err := conn.Get()
```

## Channel helpers

### OrDone

Closes the output channel when the context is cancelled or the source channel is closed:

```go
for v := range syncx.OrDoneCtx(ctx, src) {
    process(v) // stops automatically when ctx is cancelled
}
```

### Merge

Merges several channels into one:

```go
merged := syncx.Merge(ctx, ch1, ch2, ch3)
for v := range merged {
    process(v)
}
```

### FanOut

Broadcasts every value from the input channel to all output channels; sends to the various
outputs run concurrently and do not block one another.

```go
outs := syncx.FanOut(ctx, input, 3) // 3 output channels, each of them receiving every value
for _, out := range outs {
    go func(ch <-chan T) {
        for v := range ch {
            process(v)
        }
    }(out)
}
```

Every output channel receives every value in `input`. A slow consumer does not block reception by
the others. When the context is cancelled, every pending send is unblocked automatically.
