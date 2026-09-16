# retry

A retry utility supporting exponential backoff, fixed interval, linear growth, and other delay
strategies, plus custom retry conditions and callbacks.

## Features

- **Multiple delay strategies**: exponential backoff (default), fixed interval, linear growth,
  custom functions
- **Random jitter**: avoids the thundering herd effect
- **Custom retry condition**: a RetryIf function decides which errors deserve a retry
- **Retry callback**: a callback runs before each retry, handy for logging
- **Context support**: timeouts and cancellation
- **Max delay cap**: keeps delays from growing out of hand
- **Unified errors**: semantic `ErrMaxRetries` and `ErrNoRetry` errors

## Installation

```bash
go get github.com/chihqiang/infra-go/retry
```

## Quick start

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

    // retry with the default config
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

### Basic usage

```go
// use the default config (3 retries, 100ms initial delay, exponential backoff)
err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})
```

### Custom config

```go
// configure with Options
err := retry.DoWithConfig(ctx, func(ctx context.Context) error {
    return callRemoteService()
}, 
    retry.WithMaxRetries(5),           // at most 5 retries
    retry.WithDelay(200*time.Millisecond), // 200ms initial delay
    retry.WithMaxDelay(5*time.Second),     // 5s max delay
    retry.WithJitter(),                    // enable random jitter
    retry.WithOnRetry(func(attempt int, err error) {
        log.Printf("retry attempt %d: %v", attempt, err)
    }),
)
```

### Custom retry condition

```go
// retry network errors only
err := retry.DoWithConfig(ctx, func(ctx context.Context) error {
    return callRemoteService()
},
    retry.WithMaxRetries(5),
    retry.WithRetryIf(func(err error) bool {
        var netErr net.Error
        return errors.As(err, &netErr) // retry network errors only
    }),
)
```

### Delay strategies

```go
// exponential backoff (default)
retry.WithDelay(100*time.Millisecond)
// delay sequence: 100ms, 200ms, 400ms, 800ms...

// fixed interval
retry.WithDelayFunc(retry.FixedDelay(500*time.Millisecond))
// delay sequence: 500ms, 500ms, 500ms...

// linear growth
retry.WithDelayFunc(retry.LinearDelay(100*time.Millisecond, 100*time.Millisecond))
// delay sequence: 100ms, 200ms, 300ms, 400ms...

// custom exponential backoff
retry.WithDelayFunc(retry.ExponentialBackoff(10*time.Millisecond, 3))
// delay sequence: 10ms, 30ms, 90ms, 270ms...
```

### Context timeout

```go
// set an overall timeout
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})
```

### Retry callback

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

## Configuration options

| Option | Description | Default |
| ------ | ------ | -------- |
| `WithMaxRetries(n)` | Maximum number of retries | 3 |
| `WithDelay(d)` | Initial delay | 100ms |
| `WithMaxDelay(d)` | Maximum delay | 10s |
| `WithDelayFunc(fn)` | Custom delay function | Exponential backoff |
| `WithRetryIf(fn)` | Retry condition function | Retry on every error |
| `WithOnRetry(fn)` | Retry callback | None |
| `WithJitter()` | Enable random jitter | false |

### Explicit zero values: no retry / retry without waiting

The `Config` struct follows the "field == 0 means unset" rule, so **a field cannot express 0**.
When you need those semantics, pass Options to `DoWithRetryConfig` (Options are applied after
the defaults have been filled in):

```go
// no retry, run once (Config{MaxRetries: 0} would be filled in with the default of 3)
err := retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithMaxRetries(0))

// retry immediately, skipping the default 100ms initial delay
err = retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithDelay(0))

// no delay cap (Config{MaxDelay: 0} would be filled in with the default 10s)
err = retry.DoWithRetryConfig(ctx, fn, retry.Config{}, retry.WithMaxDelay(0))
```

> `Attempts(c, opts...)` accepts the same set of opts and tells you the total number of runs up
> front; `retry.Attempts(c, retry.WithMaxRetries(0))` returns `1`.
> `WithMaxDelay(0)` means "no cap", not "truncate the delay to 0".

## Error handling

```go
err := retry.Do(ctx, func(ctx context.Context) error {
    return callRemoteService()
})

switch {
case err == nil:
    // success
case retry.IsMaxRetries(err):
    // exceeded the maximum number of retries
    log.Println("max retries exceeded:", err)
case retry.IsNoRetry(err):
    // no more retries (RetryIf returned false)
    log.Println("no retry:", err)
default:
    // context cancellation, etc.
    log.Println("error:", err)
}
```

| Error | Description |
| ------ | ------ |
| `ErrMaxRetries` | Exceeded the maximum number of retries |
| `ErrNoRetry` | No more retries (RetryIf returned false) |
