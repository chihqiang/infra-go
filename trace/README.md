# trace

基于 [OpenTelemetry](https://opentelemetry.io) 的链路追踪包，提供简洁的 API 用于在服务间传递和记录链路上下文。

## 特性

- **多导出器支持**：OTLP gRPC、OTLP HTTP、Zipkin、文件输出
- **gRPC / HTTP 传播**：链路上下文在 gRPC metadata 和 HTTP header 间自动注入和提取
- **配置驱动**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **日志集成**：OpenTelemetry 内部错误自动转发到 `logger` 包
- **资源管理**：支持添加自定义资源属性（服务名、环境等）
- **全局单例**：`StartAgent` 使用 `sync.Once` 确保只初始化一次
- **采样控制**：可配置采样率（0.0~1.0）
- **零外部依赖**：封装 `attribute` 和 `trace` 包，外部无需直接导入 OpenTelemetry

## 安装

```bash
go get github.com/chihqiang/infra-go/trace
```

## 快速开始

```go
package main

import (
    "context"

    "github.com/chihqiang/infra-go/trace"
)

func main() {
    // 启动链路追踪 agent
    trace.StartAgent(trace.Config{
        Name:     "my-service",
        Endpoint: "localhost:4317", // OTLP gRPC
        Batcher:  trace.BatcherOTLPGRPC,
        Sampler:  1.0,
    })
    defer trace.StopAgent()

    // 创建 span
    ctx, span := trace.StartSpan(context.Background(), "operation-name")
    defer span.End()

    // 在 ctx 中传递链路上下文到下游调用...
    handleRequest(ctx)
}

func handleRequest(ctx context.Context) {
    ctx, span := trace.StartSpan(ctx, "handle-request")
    defer span.End()

    // trace id 可用于日志关联
    traceID := trace.TraceIDFromContext(ctx)
    println("trace id:", traceID)
}
```

## 配置

### Config 结构体

```go
trace.StartAgent(trace.Config{
    Name:           "my-service",
    Endpoint:       "localhost:4317",
    Sampler:        1.0,
    Batcher:        trace.BatcherOTLPGRPC,
    OtlpHeaders:    map[string]string{"key": "value"},
    OtlpHttpPath:   "/v1/traces",
    OtlpHttpSecure: false,
    Disabled:       false,
})
```

### 配置项说明

| 字段 | 类型 | 默认值 | 说明 |
| ------ | ------ | -------- | ------ |
| `Name` | `string` | `infra-go` | 服务名称，标识链路来源 |
| `Endpoint` | `string` | `""` | 导出器地址（file 类型为文件路径） |
| `Sampler` | `float64` | `1.0` | 采样率，0.0~1.0 |
| `Batcher` | `Batcher` | `otlpgrpc` | 导出器类型 |
| `OtlpHeaders` | `map[string]string` | `nil` | OTLP 传输自定义请求头 |
| `OtlpHttpPath` | `string` | `""` | OTLP HTTP 路径 |
| `OtlpHttpSecure` | `bool` | `false` | OTLP HTTP 是否使用 HTTPS |
| `Disabled` | `bool` | `false` | 是否禁用链路追踪 |

### 导出器类型

| 类型 | 说明 | Endpoint 示例 |
| ------ | ------ | --------------- |
| `otlpgrpc` | OTLP gRPC 导出（默认） | `localhost:4317` |
| `otlphttp` | OTLP HTTP 导出 | `localhost:4318` |
| `zipkin` | Zipkin 导出 | `http://localhost:9411/api/v2/spans` |
| `file` | 输出到文件 | `/var/log/trace.log` |

## API

### Agent 管理

```go
// 启动 agent
trace.StartAgent(cfg)

// 关闭 agent（程序退出前调用）
trace.StopAgent()
```

### Span 管理

```go
// 创建并启动 span
ctx, span := trace.StartSpan(ctx, "operation-name")
defer span.End()

// 从 context 获取 tracer
tracer := trace.TracerFromContext(ctx)

// 获取 trace id / span id
traceID := trace.TraceIDFromContext(ctx)
spanID := trace.SpanIDFromContext(ctx)
```

### gRPC 传播

```go
// 客户端：注入链路上下文到 gRPC metadata
md := metadata.Pairs()
trace.Inject(ctx, &md)
ctx = metadata.NewOutgoingContext(ctx, md)

// 服务端：从 gRPC metadata 提取链路上下文
md, _ := metadata.FromIncomingContext(ctx)
ctx, spanContext := trace.Extract(ctx, &md)
```

### HTTP 传播

```go
// 客户端：注入链路上下文到 HTTP header
req, _ := http.NewRequest("GET", "http://example.com", nil)
trace.InjectHeader(ctx, req.Header)
client.Do(req)

// 服务端：从 HTTP header 提取链路上下文
ctx, spanContext := trace.ExtractHeader(r.Context(), r.Header)
```

### 属性

本包封装了 `attribute` 包，无需直接导入 `go.opentelemetry.io/otel/attribute`：

```go
// 属性构造函数
trace.AttrString("key", "value")     // 字符串
trace.AttrInt("count", 42)           // int
trace.AttrInt64("id", 9999999999)    // int64
trace.AttrBool("enabled", true)      // bool
trace.AttrFloat64("ratio", 0.75)     // float64
trace.AttrStringSlice("tags", []string{"a", "b"})
trace.AttrIntSlice("nums", []int{1, 2, 3})
```

### Span 属性

```go
// 创建 span 时携带属性
ctx, span := trace.StartSpan(ctx, "operation",
    trace.WithAttributes(
        trace.AttrString("user", "alice"),
        trace.AttrInt("age", 30),
        trace.AttrBool("vip", true),
    ),
)
defer span.End()
```

### 资源属性

```go
// 添加自定义资源属性（在 StartAgent 之前调用）
trace.AddResources(
    trace.AttrString("env", "production"),
    trace.AttrString("region", "us-east-1"),
)
```

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "time"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/trace"
)

func main() {
    // 初始化日志
    logInstance := logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "demo",
    })
    logger.SetGlobal(logInstance)
    defer logInstance.Close()

    // 添加资源属性
    trace.AddResources(
        trace.AttrString("env", "development"),
        trace.AttrString("host", "localhost"),
    )

    // 启动链路追踪
    trace.StartAgent(trace.Config{
        Name:     "demo-service",
        Endpoint: "localhost:4317",
        Batcher:  trace.BatcherOTLPGRPC,
        Sampler:  1.0,
    })
    defer trace.StopAgent()

    // 创建根 span
    ctx, rootSpan := trace.StartSpan(context.Background(), "main-operation")
    defer rootSpan.End()

    traceID := trace.TraceIDFromContext(ctx)
    logger.Info("starting operation", logger.String("trace_id", traceID))

    // 模拟处理请求
    handleRequest(ctx)

    // 模拟 HTTP 调用
    callHTTP(ctx)

    logger.Info("operation completed", logger.String("trace_id", traceID))
}

func handleRequest(ctx context.Context) {
    ctx, span := trace.StartSpan(ctx, "handle-request",
        trace.WithAttributes(trace.AttrString("handler", "handleRequest")),
    )
    defer span.End()

    time.Sleep(10 * time.Millisecond)
    logger.Info("request handled",
        logger.String("trace_id", trace.TraceIDFromContext(ctx)),
        logger.String("span_id", trace.SpanIDFromContext(ctx)),
    )
}

func callHTTP(ctx context.Context) {
    ctx, span := trace.StartSpan(ctx, "http-call")
    defer span.End()

    // 创建 HTTP 请求并注入链路上下文
    req, _ := http.NewRequest("GET", "http://localhost:9090/api", nil)
    trace.InjectHeader(ctx, req.Header)

    // trace id 会自动通过 header 传递
    fmt.Println("trace-id header:", req.Header.Get("Traceparent"))
}
```
