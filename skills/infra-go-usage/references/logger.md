# logger

A logging package built on [go.uber.org/zap](https://github.com/uber-go/zap) and [lumberjack](https://github.com/natefinch/lumberjack), offering a concise, easy-to-use API while keeping zap's high performance, with built-in automatic log file rotation. No zap types are exposed, so users never need to import zap.

## Features

- **Interface-driven**: decoupled through the `ILogger` interface, which eases mock testing and swapping implementations between libraries
- **No zap dependency**: provides its own `Field` type and field constructors (`String`, `Int`, `Err`, etc.), so users never import zap
- **Four kinds of logging API**: structured logs, formatted logs, context-aware structured logs and context-aware formatted logs
- **Multiple output formats**: JSON encoding (production) and Console encoding (development)
- **Multiple output targets**: stdout, stderr and files; can write to several targets at once
- **Log rotation**: built on lumberjack — size-based splitting, backup count retention, age-based cleanup and optional gzip compression
- **Default-value tags**: Config defines defaults with `default` struct tags, following the conf standard
- **Automatic directory creation**: missing directories are created automatically for file output
- **Global Logger**: a built-in global instance that supports direct package-level calls
- **Context logging**: `Ctx`-suffixed methods extract fields (traceID, spanID, etc.) from `context.Context` automatically
- **Extensible extractors**: register custom context field extractors with `RegisterContextExtractor` (returns an unregister function, so it can be undone)
- **Caller information**: records the caller's file name and line number automatically (correctly skipping wrapper layers)
- **Stack traces**: optionally record stack traces at Error level and above
- **Application name**: optionally emits a fixed application name field

## Installation

```bash
go get github.com/chihqiang/infra-go/logger
```

## Quick start

```go
package main

import (
    "github.com/chihqiang/infra-go/logger"
)

func main() {
    // use the global Logger
    logger.Info("hello world")
    // output: {"level":"INFO","time":"2026-01-01T12:00:00.000+08:00","caller":"main.go:9","msg":"hello world"}

    // formatted output
    logger.Infof("user %s logged in, id=%d", "alice", 42)

    // flush the buffer before the program exits
    defer logger.Sync()
}
```

## Configuration

### Custom Logger

```go
l := logger.New(logger.Config{
    Level:       logger.InfoLevel,
    Encoding:    logger.JSONEncoding,
    Output:      []string{"stdout", "/var/log/app.log"},
    ErrorOutput: "stderr",
    Caller:      true,
    Stacktrace:  false,
    AppName:     "my-service",
    Rotation: logger.RotationConfig{
        MaxSize:    100,  // MB
        MaxBackups: 7,
        MaxAge:     30,   // days
        Compress:   true,
    },
})
```

> `New` returns the `ILogger` interface. Zero-value fields are filled with their defaults automatically; the defaults are defined by the `default` struct tag.

### Config reference

| Field | Type | Default | Tag | Description |
| ------ | ------ | -------- | ------ | ------ |
| `Level` | `Level` | `InfoLevel` | `default=0` | Log level |
| `Encoding` | `Encoding` | `JSONEncoding` | `default=json` | Encoding format: `JSONEncoding` or `ConsoleEncoding` |
| `Output` | `[]string` | `["stdout"]` | `default=[stdout]` | Output target list; accepts `"stdout"`, `"stderr"` or file paths |
| `ErrorOutput` | `string` | `"stderr"` | `default=stderr` | Target for internal error output |
| `Caller` | `bool` | `true` | `default=true` | Whether to record caller information |
| `Stacktrace` | `bool` | `false` | `optional` | Whether to record stack traces at Error level and above |
| `TimeLayout` | `string` | ISO8601 | `default=2006-01-02T15:04:05.000Z07:00` | Time layout |
| `AppName` | `string` | `""` | `optional` | Application name, emitted as the fixed field `app` |
| `Rotation` | `RotationConfig` | see below | — | Log file rotation config; only applies to file-path outputs |

#### RotationConfig

| Field | Type | Default | Tag | Description |
| ------ | ------ | -------- | ------ | ------ |
| `MaxSize` | `int` | `100` | `default=100` | Maximum size of a single log file (MB); rotation is triggered beyond it |
| `MaxBackups` | `int` | `7` | `default=7` | Maximum number of old log files to keep; the oldest are deleted beyond it |
| `MaxAge` | `int` | `30` | `default=30` | Maximum age of old log files in days; older ones are deleted |
| `Compress` | `bool` | `false` | `optional` | Whether to gzip the old log files |
| `LocalTime` | `bool` | `true` | `default=true` | Whether to use local time to name backup files; `false` uses UTC |

> The rotation config only applies to file-path `Output` entries; `"stdout"` / `"stderr"` are unaffected.

### Log levels

```go
logger.DebugLevel  // debug information
logger.InfoLevel   // general information (default)
logger.WarnLevel   // warnings
logger.ErrorLevel  // errors
logger.DPanicLevel // panics in development mode
logger.PanicLevel  // panic then exit
logger.FatalLevel  // os.Exit(1) after a fatal error
```

## The ILogger interface

All logging methods are exposed through the `ILogger` interface, which eases dependency injection and test mocks.

```go
type ILogger interface {
    // structured logging
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Panic(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // formatted logging
    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
    Panicf(format string, args ...any)
    Fatalf(format string, args ...any)

    // structured logging with context
    DebugCtx(ctx context.Context, msg string, fields ...Field)
    InfoCtx(ctx context.Context, msg string, fields ...Field)
    WarnCtx(ctx context.Context, msg string, fields ...Field)
    ErrorCtx(ctx context.Context, msg string, fields ...Field)
    PanicCtx(ctx context.Context, msg string, fields ...Field)
    FatalCtx(ctx context.Context, msg string, fields ...Field)

    // formatted logging with context
    DebugfCtx(ctx context.Context, format string, args ...any)
    InfofCtx(ctx context.Context, format string, args ...any)
    WarnfCtx(ctx context.Context, format string, args ...any)
    ErrorfCtx(ctx context.Context, format string, args ...any)
    PanicfCtx(ctx context.Context, format string, args ...any)
    FatalfCtx(ctx context.Context, format string, args ...any)

    Sync() error
}
```

## API

### Structured logging (high performance)

Pass key/value pairs with `logger.Field` for the best performance. No zap import needed.

```go
l := logger.New(logger.Config{AppName: "api-server"})

l.Info("request received",
    logger.String("method", "GET"),
    logger.String("path", "/api/users"),
    logger.Int("status", 200),
    logger.Duration("latency", 42*time.Millisecond),
)
```

Output:

```json
{"level":"INFO","time":"...","caller":"main.go:12","app":"api-server","msg":"request received","method":"GET","path":"/api/users","status":200,"latency":"42ms"}
```

#### Field constructors

| Function | Type |
| ------ | ------ |
| `logger.String(key, val)` | string |
| `logger.Int(key, val)` | int |
| `logger.Int64(key, val)` | int64 |
| `logger.Float64(key, val)` | float64 |
| `logger.Bool(key, val)` | bool |
| `logger.Duration(key, val)` | time.Duration |
| `logger.Time(key, val)` | time.Time |
| `logger.Err(err)` | error (key name is `"error"`) |
| `logger.Any(key, val)` | any |

### Formatted logging

Supports `Printf`-style format strings; the method names end with `F`.

```go
l.Infof("user %s (id=%d) logged in from %s", name, id, ip)
l.Warnf("rate limit exceeded: %d/%d", current, max)
l.Errorf("database error: %v", err)
```

### Global Logger

The package ships with a global Logger instance that can be used directly, with no setup.

```go
// structured logs
logger.Info("server started", logger.String("addr", ":8080"))
logger.Error("database connection failed", logger.Err(err))

// formatted logs
logger.Infof("listening on %s", addr)
logger.Warnf("deprecated config: %s", key)

// flush the buffer
defer logger.Sync()
```

Replacing the global Logger:

```go
l := logger.New(logger.Config{
    Level:    logger.DebugLevel,
    Encoding: logger.ConsoleEncoding,
    AppName:  "my-app",
})
logger.SetGlobal(l)
```

> **About closing**: `New` returns the `ILogger` interface, and **the interface has no `Close`** (`Close` is only defined on the concrete `*logger.Logger` type). Before the global instance exits, flushing the buffer with the package-level `logger.Sync()` is enough; if you really must close an instance manually, use a type assertion `l.(*logger.Logger).Close()`. For a hot swap at runtime use `logger.ReplaceGlobal(cfg)`, which returns the replaced instance so you can close it yourself.

### Context logging

Every logging method has a `Ctx`-suffixed version that automatically extracts fields from the `context.Context` and injects them into the log.

#### Basic usage

```go
ctx, span := trace.StartSpan(ctx, "handle-request")
defer span.End()

logger.InfofCtx(ctx, "processing request, userID: %d", userID)
// output: {"level":"INFO","msg":"processing request, userID: 42","trace_id":"5c4eff...","span_id":"6252c3..."}
```

#### Available Ctx methods

| Structured logging | Formatted logging |
| ----------- | ---------- |
| `DebugCtx(ctx, msg, fields...)` | `DebugfCtx(ctx, format, args...)` |
| `InfoCtx(ctx, msg, fields...)` | `InfofCtx(ctx, format, args...)` |
| `WarnCtx(ctx, msg, fields...)` | `WarnfCtx(ctx, format, args...)` |
| `ErrorCtx(ctx, msg, fields...)` | `ErrorfCtx(ctx, format, args...)` |
| `PanicCtx(ctx, msg, fields...)` | `PanicfCtx(ctx, format, args...)` |
| `FatalCtx(ctx, msg, fields...)` | `FatalfCtx(ctx, format, args...)` |

#### Custom context extractors

Register a custom extractor with `RegisterContextExtractor` to pull business fields out of the context.
It returns an **unregister function** that undoes the registration.

```go
// extractor function signature
type ContextExtractor func(ctx context.Context) []Field

// register an extractor (several can be registered; all results are merged when a log is emitted)
unregister := logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
    if tenantID, ok := ctx.Value("tenant_id").(string); ok {
        return []logger.Field{logger.String("tenant_id", tenantID)}
    }
    return nil
})
defer unregister() // undo it when no longer needed (idempotent; repeated calls take effect once)
```

> **Why unregister**: registration is append-only, and Go function values cannot be compared, so duplicates
> can't be detected at registration time. If the initialization flow runs more than once (retries, staged
> configuration, test reuse), the same extractor gets registered several times and the same fields appear
> repeatedly in every log line. The returned unregister function undoes it.
> Passing `nil` as the `extractor` registers nothing, and the returned unregister function is a no-op.

#### Built-in extractors

The `infra-go/trace` package registers a tracing extractor automatically in `init()`, so no manual setup is required:

```go
import _ "github.com/chihqiang/infra-go/trace" // registers the trace_id, span_id extractors automatically
```

Extracted fields:

- `trace_id`: the trace ID
- `span_id`: the current span ID

#### Extractor execution order

1. All extractors run in registration order
2. The results of all extractors are merged into a single field list
3. The `fields` arguments of a `Ctx` method are appended after the extractor fields

```go
// extractor fields first, manual fields afterwards
logger.InfoCtx(ctx, "operation completed",
    logger.Int("status", 200),
)
// output: {"trace_id":"...","span_id":"...","status":200,"msg":"operation completed"}
```

## Encoding formats

### JSON encoding (default)

Suited to production and easy for log collection systems to parse.

```json
{"level":"INFO","time":"2026-01-01T12:00:00.000+08:00","caller":"main.go:10","msg":"hello","key":"value"}
```

### Console encoding

Suited to development: human-readable and colorized.

```text
2026-01-01T12:00:00.000+0800    INFO    main.go:10    hello    {"key": "value"}
```

## Output targets

### Standard output / error

```go
logger.New(logger.Config{
    Output: []string{"stdout"},      // standard output
    ErrorOutput: "stderr",           // standard error
})
```

### File output (automatic rotation)

File output uses [lumberjack](https://github.com/natefinch/lumberjack) for automatic rotation, so log files never grow without bound.

```go
logger.New(logger.Config{
    Output: []string{"/var/log/app.log"},
    Rotation: logger.RotationConfig{
        MaxSize:    100,  // a single file is at most 100MB
        MaxBackups: 7,    // keep 7 backups
        MaxAge:     30,   // keep them for 30 days
        Compress:   true, // gzip the old files
    },
})
```

Rotation behavior:

1. When the log file exceeds `MaxSize`, the current file is renamed to `app-<timestamp>.log` (or `.log.gz` when compression is enabled)
2. A new `app.log` is created and writing continues
3. If the number of backups exceeds `MaxBackups`, the oldest backups are deleted
4. If a backup is older than `MaxAge` days, it is deleted

When `Rotation` is not set the defaults apply (100MB / 7 files / 30 days / no compression).

Missing directories are created automatically for file output.

### Multiple output targets

Writing to the console and a file at the same time:

```go
logger.New(logger.Config{
    Output: []string{"stdout", "/var/log/app.log"},
})
```

## Complete example

```go
package main

import (
    "time"

    "github.com/chihqiang/infra-go/logger"
)

func main() {
    // create the Logger
    l := logger.New(logger.Config{
        Level:      logger.InfoLevel,
        Encoding:   logger.JSONEncoding,
        Output:     []string{"stdout", "/var/log/myapp/app.log"},
        Caller:     true,
        Stacktrace: true,
        AppName:    "myapp",
        Rotation: logger.RotationConfig{
            MaxSize:    100,
            MaxBackups: 7,
            MaxAge:     30,
            Compress:   true,
        },
    })
    logger.SetGlobal(l)

    // structured logs
    logger.Info("server starting",
        logger.String("host", "0.0.0.0"),
        logger.Int("port", 8080),
    )

    // formatted logs
    logger.Infof("server shutdown after %v", time.Since(start))
}
```

## Performance recommendations

- **Production**: use the structured logging methods (`Info`, `Error`, etc.) for the best performance
- **Development**: use `ConsoleEncoding` and `DebugLevel`; the formatted methods are more convenient
- **Tracing**: use the `Ctx`-suffixed methods to inject traceID/spanID automatically, with no manual passing
- **Before exiting**: always call `Sync()` to flush the buffer so logs aren't lost
- **Rotation**: always configure `Rotation` in production so log files don't grow without bound
- **Compressed backups**: enable `Compress: true` to save disk space; old log files are gzipped automatically
