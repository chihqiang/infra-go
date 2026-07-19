# logger

基于 [go.uber.org/zap](https://github.com/uber-go/zap) 和 [lumberjack](https://github.com/natefinch/lumberjack) 的日志包，提供简洁易用的 API，同时保留 zap 的高性能，内置日志文件自动轮转功能。对外不暴露 zap 类型，用户无需导入 zap。

## 特性

- **接口驱动**：通过 `ILogger` 接口解耦，便于 mock 测试与库间替换
- **零 zap 依赖**：提供独立的 `Field` 类型和字段构造函数（`String`、`Int`、`Err` 等），用户无需导入 zap
- **四类日志 API**：结构化日志、格式化日志、带上下文结构化日志、带上下文格式化日志
- **多格式输出**：JSON 编码（生产环境）和 Console 编码（开发环境）
- **多输出目标**：支持 stdout、stderr 和文件，可同时输出到多个目标
- **日志轮转**：基于 lumberjack，按文件大小自动切割、保留备份数量、按天数清理、可选 gzip 压缩
- **默认值标签**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **自动目录创建**：文件输出时自动创建不存在的目录
- **全局 Logger**：内置全局实例，支持包级别直接调用
- **上下文日志**：`Ctx` 后缀方法自动从 `context.Context` 提取字段（traceID、spanID 等）
- **可扩展提取器**：通过 `RegisterContextExtractor` 注册自定义上下文字段提取器
- **调用者信息**：自动记录调用者的文件名和行号（正确跳过封装层）
- **堆栈追踪**：可选在 Error 及以上级别记录堆栈
- **应用名称**：可选输出固定的应用名称字段

## 安装

```bash
go get github.com/chihqiang/infra-go/logger
```

## 快速开始

```go
package main

import (
    "github.com/chihqiang/infra-go/logger"
)

func main() {
    // 使用全局 Logger
    logger.Info("hello world")
    // 输出: {"level":"INFO","time":"2026-01-01T12:00:00.000+08:00","caller":"main.go:9","msg":"hello world"}

    // 格式化输出
    logger.Infof("user %s logged in, id=%d", "alice", 42)

    // 程序退出前刷新缓冲区
    defer logger.Sync()
}
```

## 配置

### 自定义 Logger

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
        MaxAge:     30,   // 天
        Compress:   true,
    },
})
```

> `New` 返回 `ILogger` 接口。零值字段会自动填充默认值，默认值通过结构体标签 `default` 定义。

### 配置项说明

| 字段 | 类型 | 默认值 | 标签 | 说明 |
| ------ | ------ | -------- | ------ | ------ |
| `Level` | `Level` | `InfoLevel` | `default=0` | 日志级别 |
| `Encoding` | `Encoding` | `JSONEncoding` | `default=json` | 编码格式：`JSONEncoding` 或 `ConsoleEncoding` |
| `Output` | `[]string` | `["stdout"]` | `default=[stdout]` | 输出目标列表，支持 `"stdout"`、`"stderr"` 或文件路径 |
| `ErrorOutput` | `string` | `"stderr"` | `default=stderr` | 内部错误输出目标 |
| `Caller` | `bool` | `true` | `default=true` | 是否记录调用者信息 |
| `Stacktrace` | `bool` | `false` | `optional` | 是否在 Error 及以上级别记录堆栈 |
| `TimeLayout` | `string` | ISO8601 | `default=2006-01-02T15:04:05.000Z07:00` | 时间格式布局 |
| `AppName` | `string` | `""` | `optional` | 应用名称，作为固定字段 `app` 输出 |
| `Rotation` | `RotationConfig` | 见下表 | — | 日志文件轮转配置，仅对文件路径类型的 Output 生效 |

#### RotationConfig 轮转配置

| 字段 | 类型 | 默认值 | 标签 | 说明 |
| ------ | ------ | -------- | ------ | ------ |
| `MaxSize` | `int` | `100` | `default=100` | 单个日志文件最大大小（MB），超过后触发轮转 |
| `MaxBackups` | `int` | `7` | `default=7` | 保留的旧日志文件最大数量，超过后删除最旧的 |
| `MaxAge` | `int` | `30` | `default=30` | 保留旧日志文件的最大天数，超过后删除 |
| `Compress` | `bool` | `false` | `optional` | 是否用 gzip 压缩旧日志文件 |
| `LocalTime` | `bool` | `true` | `default=true` | 是否使用本地时间命名备份文件，`false` 时使用 UTC 时间 |

> 轮转配置仅对文件路径类型的 `Output` 生效，`"stdout"` / `"stderr"` 不受影响。

### 日志级别

```go
logger.DebugLevel  // 调试信息
logger.InfoLevel   // 常规信息（默认）
logger.WarnLevel   // 警告
logger.ErrorLevel  // 错误
logger.DPanicLevel // 开发模式 panic
logger.PanicLevel  // panic 后退出
logger.FatalLevel  // 致命错误后 os.Exit(1)
```

## ILogger 接口

所有日志方法通过 `ILogger` 接口统一暴露，便于依赖注入和测试 mock。

```go
type ILogger interface {
    // 结构化日志
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Panic(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // 格式化日志
    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
    Panicf(format string, args ...any)
    Fatalf(format string, args ...any)

    // 带上下文的结构化日志
    DebugCtx(ctx context.Context, msg string, fields ...Field)
    InfoCtx(ctx context.Context, msg string, fields ...Field)
    WarnCtx(ctx context.Context, msg string, fields ...Field)
    ErrorCtx(ctx context.Context, msg string, fields ...Field)
    PanicCtx(ctx context.Context, msg string, fields ...Field)
    FatalCtx(ctx context.Context, msg string, fields ...Field)

    // 带上下文的格式化日志
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

### 结构化日志（高性能）

使用 `logger.Field` 传递键值对，性能最优。无需导入 zap。

```go
l := logger.New(logger.Config{AppName: "api-server"})

l.Info("request received",
    logger.String("method", "GET"),
    logger.String("path", "/api/users"),
    logger.Int("status", 200),
    logger.Duration("latency", 42*time.Millisecond),
)
```

输出：

```json
{"level":"INFO","time":"...","caller":"main.go:12","app":"api-server","msg":"request received","method":"GET","path":"/api/users","status":200,"latency":"42ms"}
```

#### 字段构造函数

| 函数 | 类型 |
| ------ | ------ |
| `logger.String(key, val)` | string |
| `logger.Int(key, val)` | int |
| `logger.Int64(key, val)` | int64 |
| `logger.Float64(key, val)` | float64 |
| `logger.Bool(key, val)` | bool |
| `logger.Duration(key, val)` | time.Duration |
| `logger.Time(key, val)` | time.Time |
| `logger.Err(err)` | error（键名为 `"error"`） |
| `logger.Any(key, val)` | any |

### 格式化日志

支持 `Printf` 风格的格式化字符串，方法名以 `F` 结尾。

```go
l.Infof("user %s (id=%d) logged in from %s", name, id, ip)
l.Warnf("rate limit exceeded: %d/%d", current, max)
l.Errorf("database error: %v", err)
```

### 全局 Logger

包内置全局 Logger 实例，可直接使用，无需创建。

```go
// 结构化日志
logger.Info("server started", logger.String("addr", ":8080"))
logger.Error("database connection failed", logger.Err(err))

// 格式化日志
logger.Infof("listening on %s", addr)
logger.Warnf("deprecated config: %s", key)

// 刷新缓冲区
defer logger.Sync()
```

替换全局 Logger：

```go
l := logger.New(logger.Config{
    Level:    logger.DebugLevel,
    Encoding: logger.ConsoleEncoding,
    AppName:  "my-app",
})
logger.SetGlobal(l)
```

### 上下文日志

所有日志方法都有 `Ctx` 后缀版本，自动从 `context.Context` 中提取字段并注入日志。

#### 基本用法

```go
ctx, span := trace.StartSpan(ctx, "handle-request")
defer span.End()

logger.InfofCtx(ctx, "处理请求, 用户ID: %d", userID)
// 输出: {"level":"INFO","msg":"处理请求, 用户ID: 42","trace_id":"5c4eff...","span_id":"6252c3..."}
```

#### 可用的 Ctx 方法

| 结构化日志 | 格式化日志 |
| ----------- | ---------- |
| `DebugCtx(ctx, msg, fields...)` | `DebugfCtx(ctx, format, args...)` |
| `InfoCtx(ctx, msg, fields...)` | `InfofCtx(ctx, format, args...)` |
| `WarnCtx(ctx, msg, fields...)` | `WarnfCtx(ctx, format, args...)` |
| `ErrorCtx(ctx, msg, fields...)` | `ErrorfCtx(ctx, format, args...)` |
| `PanicCtx(ctx, msg, fields...)` | `PanicfCtx(ctx, format, args...)` |
| `FatalCtx(ctx, msg, fields...)` | `FatalfCtx(ctx, format, args...)` |

#### 自定义上下文提取器

通过 `RegisterContextExtractor` 注册自定义提取器，从 context 中提取业务字段。

```go
// 提取器函数签名
type ContextExtractor func(ctx context.Context) []Field

// 注册提取器（可注册多个，日志输出时合并所有结果）
logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
    if tenantID, ok := ctx.Value("tenant_id").(string); ok {
        return []logger.Field{logger.String("tenant_id", tenantID)}
    }
    return nil
})
```

#### 内置提取器

`infra-go/trace` 包在 `init()` 中自动注册链路追踪提取器，无需手动配置：

```go
import _ "github.com/chihqiang/infra-go/trace" // 自动注册 trace_id, span_id 提取器
```

提取的字段：

- `trace_id`：链路追踪 ID
- `span_id`：当前 span ID

#### 提取器执行顺序

1. 按注册顺序依次执行所有提取器
2. 所有提取器的结果合并为一个字段列表
3. `Ctx` 方法的 `fields` 参数追加在提取器字段之后

```go
// 提取器字段在前，手动字段在后
logger.InfoCtx(ctx, "操作完成",
    logger.Int("status", 200),
)
// 输出: {"trace_id":"...","span_id":"...","status":200,"msg":"操作完成"}
```

## 编码格式

### JSON 编码（默认）

适合生产环境，便于日志收集系统解析。

```json
{"level":"INFO","time":"2026-01-01T12:00:00.000+08:00","caller":"main.go:10","msg":"hello","key":"value"}
```

### Console 编码

适合开发环境，人类可读，带颜色。

```text
2026-01-01T12:00:00.000+0800    INFO    main.go:10    hello    {"key": "value"}
```

## 输出目标

### 标准输出/错误

```go
logger.New(logger.Config{
    Output: []string{"stdout"},      // 标准输出
    ErrorOutput: "stderr",           // 标准错误
})
```

### 文件输出（自动轮转）

文件输出基于 [lumberjack](https://github.com/natefinch/lumberjack) 实现自动轮转，避免日志文件无限增长。

```go
logger.New(logger.Config{
    Output: []string{"/var/log/app.log"},
    Rotation: logger.RotationConfig{
        MaxSize:    100,  // 单文件最大 100MB
        MaxBackups: 7,    // 保留 7 个备份
        MaxAge:     30,   // 保留 30 天
        Compress:   true, // gzip 压缩旧文件
    },
})
```

轮转行为：

1. 当日志文件大小超过 `MaxSize` 时，当前文件重命名为 `app-<timestamp>.log`（或 `.log.gz` 如果启用压缩）
2. 创建新的 `app.log` 继续写入
3. 如果备份数量超过 `MaxBackups`，删除最旧的备份
4. 如果备份天数超过 `MaxAge`，删除超龄的备份

不设置 `Rotation` 时使用默认值（100MB/7份/30天/不压缩）。

文件输出时会自动创建不存在的目录。

### 多目标输出

同时输出到控制台和文件：

```go
logger.New(logger.Config{
    Output: []string{"stdout", "/var/log/app.log"},
})
```

## 完整示例

```go
package main

import (
    "time"

    "github.com/chihqiang/infra-go/logger"
)

func main() {
    // 创建 Logger
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

    // 结构化日志
    logger.Info("server starting",
        logger.String("host", "0.0.0.0"),
        logger.Int("port", 8080),
    )

    // 格式化日志
    logger.Infof("server shutdown after %v", time.Since(start))
}
```

## 性能建议

- **生产环境**：使用结构化日志方法（`Info`、`Error` 等），性能最优
- **开发环境**：使用 `ConsoleEncoding` 和 `DebugLevel`，格式化方法更便捷
- **链路追踪**：使用 `Ctx` 后缀方法自动注入 traceID/spanID，无需手动传递
- **退出前**：始终调用 `Sync()` 刷新缓冲区，避免丢失日志
- **日志轮转**：生产环境务必配置 `Rotation`，避免日志文件无限增长
- **压缩备份**：启用 `Compress: true` 节省磁盘空间，旧日志文件自动 gzip 压缩
