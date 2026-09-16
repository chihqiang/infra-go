# trace — distributed tracing

A distributed tracing package built on [OpenTelemetry](https://opentelemetry.io), with a concise
API for propagating and recording trace context between services.

> The **HTTP server tracing middleware** has moved to the `httpx/middleware` subpackage
> (`middleware.NewTracing`) and is registered conveniently via `httpx.WithTracing` in the main
> httpx package (see [httpx](./httpx.md)). The `trace` package focuses on: TracerProvider assembly,
> span management, gRPC/HTTP header propagation, and attribute wrappers.

## Features

- **Multiple exporters**: OTLP gRPC, OTLP HTTP, Zipkin, file output
- **gRPC / HTTP propagation**: trace context is injected/extracted automatically between gRPC
  metadata and HTTP headers
- **Config-driven**: Config defines defaults with `default` struct tags, following the conf standard
- **Logger integration**: registers a context extractor so `logger.XxxCtx` automatically carries
  `trace_id`/`span_id`
- **Resource management**: supports adding custom resource attributes (service name, environment, etc.)
- **Global singleton**: `StartAgent` manages the lifecycle with a lock plus `currentAgent`;
  repeated calls ignore the new config and log a warning. After `StopAgent` you can call
  `StartAgent` again (the old implementation used `sync.Once`, which made restarting impossible)
- **Sampling control**: configurable sample rate (`0`–`1.0`); note that `Config.Sampler = 0` counts
  as unset and falls back to `1.0`. For "do not sample proactively, only follow the upstream" use
  `trace.StartAgent(cfg, trace.WithSampler(0))`, which is **not** equivalent to `Disabled`
- **Convenient wrappers**: wraps the `attribute`/`trace` types, so everyday calls need no direct
  OpenTelemetry API (only explicit return-type declarations require importing
  `go.opentelemetry.io/otel/trace`)

## Installation

```bash
go get github.com/chihqiang/infra-go/trace
```

## Quick start

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
    // logger.XxxCtx carries trace_id / span_id automatically (importing trace wires up the extractor)
    logger.InfoCtx(ctx, "handle", logger.String("handler", "main"))
}
```

> Automatic instrumentation on the HTTP server: `httpx.WithTracing()` (register it before
> `WithLogger` so access logs carry `trace_id`).

## Configuration

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

| Field | Type | Default | Description |
|------|------|--------|------|
| `Name` | `string` | `infra-go` | Service name, identifying the trace source |
| `Endpoint` | `string` | `""` | Exporter address (a file path for the file type) |
| `Sampler` | `float64` | `1.0` | Sample rate (`0`–`1.0`); a literal `0` is ignored and falls back to `1.0`, use `WithSampler(0)` if you need `0` |
| `Batcher` | `Batcher` | `otlpgrpc` | Exporter type |
| `OtlpHeaders` | `map[string]string` | `nil` | Custom request headers for OTLP transport |
| `OtlpHttpPath` | `string` | `""` | OTLP HTTP path |
| `OtlpHttpSecure` | `bool` | `false` | Whether OTLP HTTP uses HTTPS |
| `OtlpGrpcSecure` | `bool` | `false` | Whether OTLP gRPC uses TLS (connecting to a TLS collector) |
| `Disabled` | `bool` | `false` | Whether to disable tracing (no TracerProvider is created) |

### Sample rate 0: `WithSampler(0)` vs `Disabled`

`fillDefault` follows the "field == 0 means unset" rule, so `Config{Sampler: 0}` is filled in as
`1.0`. Use the Option form when you need `0` (it is applied after the defaults are filled in):

```go
// the root span is not sampled, but traces already sampled upstream keep being reported
// (a common cost saver)
trace.StartAgent(cfg, trace.WithSampler(0))
```

The two have different semantics, so do not substitute one for the other:

| Config | TracerProvider | Root span | Traces already sampled upstream |
|------|----------------|---------|------------------|
| `WithSampler(0)` | Created normally | Not sampled | Keep being reported (`ParentBased`) |
| `Disabled: true` | Not created | — | Not reported |

| Exporter type | Description | Endpoint example |
|------|------|---------------|
| `otlpgrpc` | OTLP gRPC export (default) | `localhost:4317` |
| `otlphttp` | OTLP HTTP export | `localhost:4318` |
| `zipkin` | Zipkin export | `http://localhost:9411/api/v2/spans` |
| `file` | Output to a file | `/var/log/trace.log` |

## API

### Agent and span

```go
trace.StartAgent(cfg)  // start (global singleton)
trace.StopAgent()      // stop (call before the program exits)

ctx, span := trace.StartSpan(ctx, "op") // create and start a span
defer span.End()

tracer  := trace.TracerFromContext(ctx) // get the tracer from the context
traceID := trace.TraceIDFromContext(ctx) // trace id (for correlating logs)
spanID  := trace.SpanIDFromContext(ctx)
```

### gRPC propagation

```go
// client: inject the trace context into gRPC metadata
md := metadata.Pairs()
trace.Inject(ctx, &md)
ctx = metadata.NewOutgoingContext(ctx, md)

// server: extract the trace context from gRPC metadata
md, _ := metadata.FromIncomingContext(ctx)
ctx, spanContext := trace.Extract(ctx, &md)
```

### HTTP propagation (client inject / server extract)

```go
// client: inject the trace context into HTTP headers
req, _ := http.NewRequest("GET", "http://example.com", nil)
trace.InjectHeader(ctx, req.Header) // writes Traceparent
client.Do(req)

// server: extract from HTTP headers (manual integration for non-httpx frameworks)
ctx, spanContext := trace.ExtractHeader(r.Context(), r.Header)
```

### HTTP server middleware (moved)

The HTTP server tracing middleware now lives in the `httpx/middleware` subpackage; register it
with `httpx.WithTracing(ignorePaths...)` and it automatically: extracts the upstream span context
(W3C traceparent) → creates a server span (carrying semantic attributes such as method/path/status)
→ injects the context so downstream code can correlate `trace_id`:

```go
// httpx
server.Use(httpx.WithTracing())                            // trace everything
server.Use(httpx.WithTracing("/health*", "/metrics/*"))   // skip probes

// standard net/http / other frameworks
import "github.com/chihqiang/infra-go/httpx/middleware"
handler := middleware.NewTracing("/health*", "/metrics/*").Middleware()(mux)
http.ListenAndServe(":8080", handler)
```

### Attributes

Wraps the `attribute` package, so `go.opentelemetry.io/otel/attribute` never needs to be imported
directly:

```go
trace.AttrString("key", "value")       // string
trace.AttrInt("count", 42)             // int
trace.AttrInt64("id", 9999999999)      // int64
trace.AttrBool("enabled", true)        // bool
trace.AttrFloat64("ratio", 0.75)       // float64
trace.AttrStringSlice("tags", []string{"a", "b"})
trace.AttrIntSlice("nums", []int{1, 2, 3})
```

Attach attributes when creating a span:

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

### Resource attributes

```go
// add custom resource attributes (call before StartAgent)
trace.AddResources(
    trace.AttrString("env", "production"),
    trace.AttrString("region", "us-east-1"),
)
```

## Full example

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
    // the ILogger interface has no Close (Close exists only on the *Logger concrete type);
    // flush buffers with the package-level Sync before exiting
    defer logger.Sync()

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
    trace.InjectHeader(ctx, req.Header) // writes Traceparent
    fmt.Println("trace-id header:", req.Header.Get("Traceparent"))
}
```
