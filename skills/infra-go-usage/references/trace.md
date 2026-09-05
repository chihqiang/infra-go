# trace — 链路追踪

基于 [OpenTelemetry](https://opentelemetry.io) 的链路追踪包，提供简洁 API 用于在服务间传递与记录链路上下文。

> **HTTP 服务端追踪中间件**已迁至 `httpx/middleware` 子包（`middleware.NewTracing`），并由 httpx 主包 `httpx.WithTracing` 便捷注册（见 [httpx](./httpx.md)）。`trace` 包专注：TracerProvider 装配、span 管理、gRPC/HTTP 头传播与属性封装。

## 特性

- **多导出器支持**：OTLP gRPC、OTLP HTTP、Zipkin、文件输出
- **gRPC / HTTP 传播**：链路上下文在 gRPC metadata 与 HTTP header 间自动注入/提取
- **配置驱动**：Config 用 `default` 结构体标签定义默认值，遵循 conf 标准
- **日志集成**：注册 context 提取器，`logger.XxxCtx` 自动携带 `trace_id`/`span_id`
- **资源管理**：支持添加自定义资源属性（服务名、环境等）
- **全局单例**：`StartAgent` 用 `sync.Once` 确保只初始化一次
- **采样控制**：可配置采样率（0.0~1.0）
- **零外部依赖**：封装 `attribute` 与 `trace` 包，外部无需直接导入 OpenTelemetry

## 安装

```bash
go get github.com/chihqiang/infra-go/trace
```

## 快速开始

```go
import "github.com/chihqiang/infra-go/trace"

func main() {
    trace.StartAgent(trace.Config{
        Name:     "my-service",
        Endpoint: "localhost:4317", // OTLP gRPC
        Batcher:  trace.BatcherOTLPGRPC,
        Sampler:  1.0,
    })
    defer trace.StopAgent()

    ctx, span := trace.StartSpan(context.Background(), "operation-name")
    defer span.End()

    traceID := trace.TraceIDFromContext(ctx)
    // logger.XxxCtx 会自动带 trace_id / span_id（空导入 trace 即注册提取器）
    logger.InfoCtx(ctx, "handle", logger.String("handler", "main"))
}
```

> HTTP 服务端自动埋点：`httpx.WithTracing()`（在 `WithLogger` 前注册，使访问日志带上 `trace_id`）。

## 配置

```go
trace.StartAgent(trace.Config{
    Name:           "my-service",
    Endpoint:       "localhost:4317",
    Sampler:        1.0,
    Batcher:        trace.BatcherOTLPGRPC,
    OtlpHeaders:    map[string]string{"key": "value"},
    OtlpHttpPath:   "/v1/traces",
    OtlpHttpSecure: false,
    OtlpGrpcSecure: false,
    Disabled:       false,
})
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Name` | `string` | `infra-go` | 服务名称，标识链路来源 |
| `Endpoint` | `string` | `""` | 导出器地址（file 类型为文件路径） |
| `Sampler` | `float64` | `1.0` | 采样率，0.0~1.0 |
| `Batcher` | `Batcher` | `otlpgrpc` | 导出器类型 |
| `OtlpHeaders` | `map[string]string` | `nil` | OTLP 传输自定义请求头 |
| `OtlpHttpPath` | `string` | `""` | OTLP HTTP 路径 |
| `OtlpHttpSecure` | `bool` | `false` | OTLP HTTP 是否使用 HTTPS |
| `OtlpGrpcSecure` | `bool` | `false` | OTLP gRPC 是否使用 TLS（连接 TLS collector） |
| `Disabled` | `bool` | `false` | 是否禁用链路追踪 |

| 导出器类型 | 说明 | Endpoint 示例 |
|------|------|---------------|
| `otlpgrpc` | OTLP gRPC 导出（默认） | `localhost:4317` |
| `otlphttp` | OTLP HTTP 导出 | `localhost:4318` |
| `zipkin` | Zipkin 导出 | `http://localhost:9411/api/v2/spans` |
| `file` | 输出到文件 | `/var/log/trace.log` |

## API

### Agent 与 Span

```go
trace.StartAgent(cfg)  // 启动（全局单例）
trace.StopAgent()      // 关闭（程序退出前调用）

ctx, span := trace.StartSpan(ctx, "op") // 创建并启动 span
defer span.End()

tracer  := trace.TracerFromContext(ctx) // 从 context 获取 tracer
traceID := trace.TraceIDFromContext(ctx) // trace id（日志关联用）
spanID  := trace.SpanIDFromContext(ctx)
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

### HTTP 传播（客户端发起 / 服务端提取）

```go
// 客户端：注入链路上下文到 HTTP header
req, _ := http.NewRequest("GET", "http://example.com", nil)
trace.InjectHeader(ctx, req.Header) // 写入 Traceparent
client.Do(req)

// 服务端：从 HTTP header 提取（供非 httpx 的框架手动接入）
ctx, spanContext := trace.ExtractHeader(r.Context(), r.Header)
```

### HTTP 服务端中间件（已迁移）

HTTP 服务端追踪中间件现位于 `httpx/middleware` 子包，经 `httpx.WithTracing(ignorePaths...)` 注册即可，自动完成：提取上游 span 上下文（W3C traceparent）→ 创建服务端 span（携带 method/path/status 等语义属性）→ 注入 context 供下游关联 `trace_id`：

```go
// httpx
server.Use(httpx.WithTracing())                            // 追踪全部
server.Use(httpx.WithTracing("/health*", "/metrics/*"))   // 跳过探活

// 标准 net/http / 其它框架
import "github.com/chihqiang/infra-go/httpx/middleware"
handler := middleware.NewTracing("/health*", "/metrics/*").Middleware()(mux)
http.ListenAndServe(":8080", handler)
```

### 属性

封装 `attribute` 包，无需直接导入 `go.opentelemetry.io/otel/attribute`：

```go
trace.AttrString("key", "value")       // 字符串
trace.AttrInt("count", 42)             // int
trace.AttrInt64("id", 9999999999)      // int64
trace.AttrBool("enabled", true)        // bool
trace.AttrFloat64("ratio", 0.75)       // float64
trace.AttrStringSlice("tags", []string{"a", "b"})
trace.AttrIntSlice("nums", []int{1, 2, 3})
```

创建 span 时携带属性：

```go
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
    logInstance := logger.New(logger.Config{Level: logger.InfoLevel, AppName: "demo"})
    logger.SetGlobal(logInstance)
    defer logInstance.Close()

    trace.AddResources(trace.AttrString("env", "development"))
    trace.StartAgent(trace.Config{
        Name: "demo-service", Endpoint: "localhost:4317",
        Batcher: trace.BatcherOTLPGRPC, Sampler: 1.0,
    })
    defer trace.StopAgent()

    ctx, rootSpan := trace.StartSpan(context.Background(), "main-operation")
    defer rootSpan.End()

    handleRequest(ctx)
    callHTTP(ctx)
}

func handleRequest(ctx context.Context) {
    ctx, span := trace.StartSpan(ctx, "handle-request",
        trace.WithAttributes(trace.AttrString("handler", "handleRequest")))
    defer span.End()
    time.Sleep(10 * time.Millisecond)
    logger.InfoCtx(ctx, "request handled")
}

func callHTTP(ctx context.Context) {
    ctx, span := trace.StartSpan(ctx, "http-call")
    defer span.End()

    req, _ := http.NewRequest("GET", "http://localhost:9090/api", nil)
    trace.InjectHeader(ctx, req.Header) // 写入 Traceparent
    fmt.Println("trace-id header:", req.Header.Get("Traceparent"))
}
```
