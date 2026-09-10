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

缓冲 handler 写入的响应，便于结束后统一加密输出（`cryption_writer.go`），`maxBufBytes` 限制缓冲上限避免 OOM，供 `middleware.Cryption` 使用。

一旦缓冲超过 `maxBufBytes`，会**切换为明文透传模式**：先把已缓冲内容原样写到底层，之后所有写入直接透传（此时 `Flush` 也透传底层，支持大响应流式输出），保证客户端拿到完整响应、不会被截断。缓冲模式下 `Flush` 为空操作——加密需整体缓冲后输出，避免在数据未就绪时向底层透传造成"半发送"状态：

```go
cw := respw.NewCryptionWriter(w, maxBytes)
next.ServeHTTP(cw, r)

code := cw.StatusCode()
if code == 0 {
    code = http.StatusOK
}

// 缓冲超限：writer 已进入明文透传并写完全部内容，此处必须直接返回，不可再写
if cw.Overflowed() {
    return
}

// 非加密场景（非 2xx、204/205、HEAD）：明文透传，保留状态码
w.Header().Del("Content-Length")
w.WriteHeader(code)
_, _ = w.Write(cw.Buffered())

// 加密场景：对 cw.Buffered() 统一加密后写回（清理 Content-Length、设置 Content-Type）
encrypted, _ := hash.AESGCMEncrypt(key, cw.Buffered())
w.Header().Set("Content-Type", "text/plain; charset=utf-8")
w.WriteHeader(code)
_, _ = w.Write([]byte(encrypted))
```

## NotFoundResponseWriter — 404 拦截

拦截底层 `ResponseWriter` 写入的 404，转发给自定义 404 handler（`notfound_writer.go`），并透传 `Flush` / `Hijack` / `Push` / `Unwrap`。

> 注意：它无法区分「路由未匹配」与「业务主动返回 404」，会一并劫持业务 404。`httpx.Server` 的 `SetNotFoundHandler` 因此**不再使用该包装器**，而是通过 `ServeMux.Handler(r)` 预判路由是否命中（同时排除 405），只对真正未匹配的请求生效。业务侧如需同样的语义，推荐直接使用 `SetNotFoundHandler`。

## 可选接口透传

包装器对不同可选接口的透传能力如下（`RecorderWriter` 与 `NotFoundResponseWriter` 完整透传全部四种；`TimeoutWriter` 与 `CryptionWriter` 因缓冲/加密语义受限）：

| 接口 | 方法 | `RecorderWriter` | `TimeoutWriter` | `CryptionWriter` | `NotFoundResponseWriter` | 场景 |
|------|------|:---:|:---:|:---:|:---:|------|
| `http.ResponseController` | `Unwrap()` | ✅ | ❌ | ❌ | ✅ | 运行时能力协商 |
| `http.Flusher` | `Flush()` | ✅ | ✅ | ⚠️ 仅透传模式 | ✅ | SSE 等流式响应（底层不支持时静默忽略） |
| `http.Hijacker` | `Hijack()` | ✅ | ✅ | ❌ | ✅ | WebSocket 升级等连接接管（不支持时返回错误） |
| `http.Pusher` | `Push()` | ✅ | ❌ | ❌ | ✅ | HTTP/2 Server Push（不支持时返回错误） |

> `CryptionWriter` 在缓冲未超限时 `Flush` 为空操作——加密需拿到完整响应体；缓冲超限后进入明文透传模式，此时 `Flush` 会透传底层。

> 若需在超时/加密包装（`TimeoutWriter`/`CryptionWriter`）之上使用 WebSocket(`Hijack`)、HTTP/2 Push 或 `http.ResponseController`，应避免经这两类中间件包装或在其外层自行处理，防止能力静默失效。

## 应用

`httpx.With*` 中间件已内部使用上述包装器（核心逻辑在 `httpx/middleware` 子包，见 [httpx](./httpx.md)），业务侧一般无需直接使用；如需自定义包装 ResponseWriter（如自定义日志统计）可直接使用本包。
