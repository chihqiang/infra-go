# httpx/respw

`http.ResponseWriter` 的增强包装工具集，位于 `httpx/respw` 子包，一类一文件。由 `httpx/middleware` 各中间件（访问日志 / 熔断 / 超时 / 加密 / 链路追踪）与 httpx 服务器（自定义 404）内部复用，避免各包重复实现导致能力漂移（如漏透传 `Flush`/`Hijack` 等可选接口）。

```go
import "github.com/chihqiang/infra-go/httpx/respw"
```

## RecorderWriter — 捕获状态码与字节数

透明包装 ResponseWriter，捕获响应状态码与写入字节数（`recorder_writer.go`），供 `middleware.AccessLogger` / `middleware.Breaker` / `middleware.Tracing` 等使用：

```go
rec := respw.NewRecorderWriter(w)
next.ServeHTTP(rec, r)

code  := rec.Status() // 实际响应状态码（未显式 WriteHeader 时为 200）
bytes := rec.Bytes()  // 累计写入的响应字节数
```

## TimeoutWriter — 超时缓冲丢弃

缓存 handler 写入的响应，支持超时后的安全丢弃（`timeout_writer.go`），实现 `http.Flusher` / `http.Hijacker`，兼容流式与 WebSocket，供 `middleware.Timeout` 使用：

```go
tw := respw.NewTimeoutWriter(w)
next.ServeHTTP(tw, r) // handler 写入先进入内存缓冲

// handler 正常结束时：把响应头/状态码/响应体写到底层 ResponseWriter
tw.Done()

// 请求超时后：标记丢弃，此后 Write 返回 http.ErrHandlerTimeout
tw.Timeout()
```

## CryptionWriter — 响应加密缓冲

缓冲 handler 写入的响应，便于结束后统一加密输出（`cryption_writer.go`），`maxBufBytes` 限制缓冲上限避免 OOM，供 `middleware.Cryption` 使用：

```go
cw := respw.NewCryptionWriter(w, maxBytes)
next.ServeHTTP(cw, r)

if cw.Overflowed() {
    // 缓冲超限：回退为明文输出（StatusCode() / Buffered() 可取已写内容）
    if cw.StatusCode() != 0 {
        w.WriteHeader(cw.StatusCode())
    }
    _, _ = w.Write(cw.Buffered())
    return
}

// 正常：对 cw.Buffered() 统一加密后写回
encrypted, _ := hash.AESGCMEncrypt(key, cw.Buffered())
_, _ = w.Write([]byte(encrypted))
```

## NotFoundResponseWriter — 自定义 404

拦截未命中路由的响应写入，转发给自定义 404 handler（`notfound_writer.go`），由 httpx 服务器 `SetNotFoundHandler` 内部使用，业务侧一般无需直接接触。

## 可选接口透传

上述包装器正确透传可选接口，避免包装导致 SSE 流式、WebSocket 升级、HTTP/2 Push 等功能静默失效：

| 接口 | 方法 | 场景 |
|------|------|------|
| `http.ResponseController` | `Unwrap()` | 运行时能力协商 |
| `http.Flusher` | `Flush()` | SSE 等流式响应（底层不支持时静默忽略） |
| `http.Hijacker` | `Hijack()` | WebSocket 升级等连接接管（不支持时返回错误） |
| `http.Pusher` | `Push()` | HTTP/2 Server Push（不支持时返回错误） |

## 应用

`httpx.With*` 中间件已内部使用上述包装器（核心逻辑在 `httpx/middleware` 子包，见 [httpx](./httpx.md)），业务侧一般无需直接使用；如需自定义包装 ResponseWriter（如自定义日志统计）可直接使用本包。
