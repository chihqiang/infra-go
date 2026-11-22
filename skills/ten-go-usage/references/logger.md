# logger

A structured logging package implemented with the Go standard library, offering a concise API with JSON/plain encoding and built-in size- or daily-based log file rotation. It has no third-party logging dependencies.

## Features

- **Interface-driven**: decoupled through the `ILogger` interface, which eases mock testing and swapping implementations between libraries
- **Standard-library implementation**: provides its own `Field` type and field constructors (`String`, `Int`, `Err`, etc.); no third-party logging library is imported
- **Four kinds of logging API**: structured logs, formatted logs, context-aware structured logs and context-aware formatted logs
- **Two encodings**: `json` (production) and `plain` (development)
- **Output modes**: console, file and volume (`Path/<ServiceName>/<hostname>/`)
- **Log rotation**: size-based or daily splitting, backup count retention, age-based cleanup and optional gzip compression
- **Default-value tags**: Config defines defaults with `default` struct tags, following the conf standard
- **Automatic directory creation**: missing directories are created automatically for file output
- **Global Logger**: a built-in global instance that supports direct package-level calls
- **Context logging**: `Ctx`-suffixed methods extract fields (traceID, spanID, etc.) from `context.Context` automatically
- **Extensible extractors**: register custom context field extractors with `RegisterContextExtractor` (returns an unregister function, so it can be undone)
- **Caller information**: records the caller's file name and line number automatically (correctly skipping wrapper layers)
- **Stack traces**: record stack traces at Error level and above, throttled by `StackCooldownMillis`
- **Service name**: emitted as the `app` field and used for volume paths

## Installation

```bash
go get github.com/chihqiang/ten-go/core/logger
```

## Quick start

```go
package main

import (
    "github.com/chihqiang/ten-go/core/logger"
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
    ServiceName: "my-service",
    Mode:        "file",
    Encoding:    "json",
    TimeFormat:  "2006-01-02T15:04:05.000Z07:00",
    Path:        "/var/log/app",
    Level:       "info",
    Rotation:    "size",
    MaxSize:     100, // MB
    MaxBackups:  7,
    KeepDays:    30,
    Compress:    true,
})
```

> `New` returns the `ILogger` interface. Zero-value fields are filled with their defaults automatically.

### Config reference

```go
type Config struct {
    ServiceName         string `json:",optional"`
    Mode                string `json:",default=console,options=[console,file,volume]"`
    Encoding            string `json:",default=json,options=[json,plain]"`
    TimeFormat          string `json:",optional"`
    Path                string `json:",default=logs"`
    Level               string `json:",default=info,options=[debug,info,error,severe]"`
    MaxContentLength    uint32 `json:",optional"`
    Compress            bool   `json:",optional"`
    Stat                bool   `json:",default=true"`
    KeepDays            int    `json:",optional"`
    StackCooldownMillis int    `json:",default=100"`
    MaxBackups          int    `json:",default=0"`
    MaxSize             int    `json:",default=0"`
    Rotation            string `json:",default=daily,options=[daily,size]"`
}
```

| Field | Default | Description |
| ------ | ------- | ----------- |
| `ServiceName` | `""` | Service name; volume mode uses it in the log path, and it is emitted as the `app` field |
| `Mode` | `console` | `console`, `file` or `volume` |
| `Encoding` | `json` | `json` or `plain` |
| `TimeFormat` | ISO8601 | Timestamp layout |
| `Path` | `logs` | Log directory for file/volume modes |
| `Level` | `info` | `debug`, `info`, `error` or `severe` (`warn` is also accepted); `severe` keeps only fatal entries |
| `MaxContentLength` | `0` | Truncate messages to this many bytes; zero means no limit |
| `Compress` | `false` | Gzip rotated backups |
| `Stat` | `true` | Kept for configuration compatibility |
| `KeepDays` | `0` | Delete backups older than this many days; zero keeps forever |
| `StackCooldownMillis` | `100` | Minimum interval between stack traces for error-level entries |
| `MaxBackups` | `0` | Maximum number of backups kept; zero keeps all |
| `MaxSize` | `0` | Maximum active file size in MB for size rotation; zero means no limit |
| `Rotation` | `daily` | `daily` or `size` |

File mode writes `access.log`, `error.log` and `severe.log` under `Path`; volume mode writes
under `Path/<ServiceName>/<hostname>/`. Console mode writes access logs to stdout and
error/severe logs to stderr. Caller information is always included.

### Log levels

`Config.Level` accepts the strings `debug`, `info`, `error` and `severe`; `warn` is accepted as an
extension. `severe` maps to `logger.FatalLevel`, because fatal entries are the ones routed to
`severe.log`. The exported `logger.DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`,
`DPanicLevel`, `PanicLevel` and `FatalLevel` constants remain available for code that compares
levels internally.

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

Pass key/value pairs with `logger.Field` for structured output.

```go
l := logger.New(logger.Config{ServiceName: "api-server"})

l.Info("request received",
    logger.String("method", "GET"),
    logger.String("path", "/api/users"),
    logger.Int("status", 200),
    logger.Duration("latency", 42*time.Millisecond),
)
```

Output:

```json
{"level":"INFO","time":"...","caller":"main.go:12","app":"api-server","msg":"request received","method":"GET","path":"/api/users","status":200,"latency":42}
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
    Level:       "debug",
    Encoding:    "plain",
    ServiceName: "my-app",
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

The `ten-go/trace` package registers a tracing extractor automatically in `init()`, so no manual setup is required:

```go
import _ "github.com/chihqiang/ten-go/core/trace" // registers the trace_id, span_id extractors automatically
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

Suited to development: human-readable plain text.

```text
2026-01-01T12:00:00.000+0800    INFO    main.go:10    hello    {"key": "value"}
```

## Output modes

### Console

```go
logger.New(logger.Config{Mode: "console", Encoding: "plain"})
```

Console mode writes access logs to stdout and error/severe logs to stderr.

### File (automatic rotation)

```go
logger.New(logger.Config{
    ServiceName: "my-service",
    Mode:        "file",
    Path:        "/var/log/my-service",
    Rotation:    "size",
    MaxSize:     100, // a single file is at most 100MB
    MaxBackups:  7,   // keep 7 backups
    KeepDays:    30,  // keep them for 30 days
    Compress:    true,
})
```

File mode creates:

```text
/var/log/my-service/
├── access.log
├── error.log
└── severe.log
```

Rotation behavior:

1. In `size` mode, the active file is rotated before a write that would exceed `MaxSize`.
   In `daily` mode it rotates when the date changes.
2. The active file is renamed to `access-<timestamp>.log` (or `.log.gz` when compression is enabled)
   and a new file is opened.
3. Backups beyond `MaxBackups` are deleted.
4. Backups older than `KeepDays` days are deleted.

Defaults are `Rotation=daily`, `MaxSize=0`, `MaxBackups=0`, `KeepDays=0`, no compression.
Missing directories are created automatically.

### Volume

Volume mode is intended for containers and writes to `Path/<ServiceName>/<hostname>/`:

```go
logger.New(logger.Config{
    ServiceName: "my-service",
    Mode:        "volume",
    Path:        "/var/log",
})
```

## Complete example

```go
package main

import (
    "time"

    "github.com/chihqiang/ten-go/core/logger"
)

func main() {
    l := logger.New(logger.Config{
        ServiceName: "myapp",
        Mode:        "file",
        Encoding:    "json",
        Path:        "/var/log/myapp",
        Level:       "info",
        Rotation:    "size",
        MaxSize:     100,
        MaxBackups:  7,
        KeepDays:    30,
        Compress:    true,
    })
    logger.SetGlobal(l)

    logger.Info("server starting",
        logger.String("host", "0.0.0.0"),
        logger.Int("port", 8080),
    )
    logger.Infof("server shutdown after %v", time.Since(start))
}
```

## Performance recommendations

- **Production**: use the structured logging methods (`Info`, `Error`, etc.) for the best performance
- **Development**: use `Encoding: "plain"` and `Level: "debug"`; the formatted methods are more convenient
- **Tracing**: use the `Ctx`-suffixed methods to inject traceID/spanID automatically, with no manual passing
- **Before exiting**: always call `Sync()` to flush the buffer so logs aren't lost
- **Rotation**: always configure `Rotation` in production so log files don't grow without bound
- **Compressed backups**: enable `Compress: true` to save disk space; old log files are gzipped automatically
