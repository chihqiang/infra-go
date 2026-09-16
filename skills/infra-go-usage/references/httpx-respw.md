# httpx/respw

A set of enhanced wrappers around `http.ResponseWriter`, living in the `httpx/respw` subpackage with one type per file. Reused internally by the `httpx/middleware` middlewares (access log / breaker / timeout / cryption / tracing) and by the httpx server (custom 404), so that each package doesn't reimplement them and drift apart (e.g. forgetting to pass through optional interfaces such as `Flush`/`Hijack`).

```go
import "github.com/chihqiang/infra-go/httpx/respw"
```

## RecorderWriter — capture status code and byte count

Transparently wraps a ResponseWriter to capture the response status code and the number of bytes written (`recorder_writer.go`); used by `middleware.AccessLogger` / `middleware.Breaker` / `middleware.Tracing` and others:

```go
rec := respw.NewRecorderWriter(w)
next.ServeHTTP(rec, r)

code  := rec.Status() // actual response status code (200 when WriteHeader was never called)
bytes := rec.Bytes()  // total number of response bytes written
```

## TimeoutWriter — timeout buffering and discarding

Buffers the response written by the handler and supports safely discarding it after a timeout (`timeout_writer.go`). Implements `http.Flusher` / `http.Hijacker`, so it stays compatible with streaming and WebSocket; used by `middleware.Timeout`:

```go
tw := respw.NewTimeoutWriter(w)
next.ServeHTTP(tw, r) // handler writes go into an in-memory buffer first

// When the handler finishes normally: write headers/status/body to the underlying ResponseWriter
tw.Done()

// After the request times out: mark as discarded; subsequent Write returns http.ErrHandlerTimeout
tw.Timeout()
```

## CryptionWriter — response encryption buffer

Buffers the response written by the handler so it can be encrypted and emitted as a whole afterwards (`cryption_writer.go`); `maxBufBytes` caps the buffer to avoid OOM. Used by `middleware.Cryption`.

Once the buffer exceeds `maxBufBytes` it **switches to plain-text passthrough mode**: the already buffered content is written to the underlying writer as-is and every subsequent write is passed straight through (in this mode `Flush` also passes through, supporting large streaming responses), guaranteeing the client receives the complete response without truncation. In buffering mode `Flush` is a no-op — encryption requires the whole response to be buffered first, which avoids passing data through to the underlying writer before it's ready and producing a "half-sent" state:

```go
cw := respw.NewCryptionWriter(w, maxBytes)
next.ServeHTTP(cw, r)

code := cw.StatusCode()
if code == 0 {
    code = http.StatusOK
}

// Buffer overflowed: the writer already switched to plain-text passthrough and wrote everything,
// so return here immediately and write nothing more
if cw.Overflowed() {
    return
}

// Non-encrypted case (non-2xx, 204/205, HEAD): plain-text passthrough, keep the status code
w.Header().Del("Content-Length")
w.WriteHeader(code)
_, _ = w.Write(cw.Buffered())

// Encrypted case: encrypt cw.Buffered() as a whole and write it back (clear Content-Length, set Content-Type)
encrypted, _ := hash.AESGCMEncrypt(key, cw.Buffered())
w.Header().Set("Content-Type", "text/plain; charset=utf-8")
w.WriteHeader(code)
_, _ = w.Write([]byte(encrypted))
```

## NotFoundResponseWriter — 404 interception

Intercepts 404s written to the underlying `ResponseWriter` and forwards them to a custom 404 handler (`notfound_writer.go`), passing through `Flush` / `Hijack` / `Push` / `Unwrap`.

> Note: it cannot distinguish "route not matched" from "the business deliberately returned 404", so it hijacks business 404s as well. `httpx.Server`'s `SetNotFoundHandler` therefore **no longer uses this wrapper**; instead it pre-checks whether the route matched via `ServeMux.Handler(r)` (also excluding 405) and only applies to requests that genuinely didn't match. Business code wanting the same semantics should use `SetNotFoundHandler` directly.

## Optional interface passthrough

The passthrough capabilities of each wrapper for the various optional interfaces are as follows (`RecorderWriter` and `NotFoundResponseWriter` pass all four through completely; `TimeoutWriter` and `CryptionWriter` are constrained by their buffering/encryption semantics):

| Interface | Method | `RecorderWriter` | `TimeoutWriter` | `CryptionWriter` | `NotFoundResponseWriter` | Scenario |
|------|------|:---:|:---:|:---:|:---:|------|
| `http.ResponseController` | `Unwrap()` | ✅ | ❌ | ❌ | ✅ | Runtime capability negotiation |
| `http.Flusher` | `Flush()` | ✅ | ✅ | ⚠️ passthrough mode only | ✅ | Streaming responses such as SSE (silently ignored when unsupported by the underlying writer) |
| `http.Hijacker` | `Hijack()` | ✅ | ✅ | ❌ | ✅ | Connection takeover such as WebSocket upgrade (returns an error when unsupported) |
| `http.Pusher` | `Push()` | ✅ | ❌ | ❌ | ✅ | HTTP/2 Server Push (returns an error when unsupported) |

> While its buffer is within the limit, `CryptionWriter` makes `Flush` a no-op — encryption needs the complete response body; once the buffer overflows it enters plain-text passthrough mode and `Flush` passes through to the underlying writer.

> If you need WebSocket (`Hijack`), HTTP/2 Push or `http.ResponseController` on top of the timeout/encryption wrappers (`TimeoutWriter`/`CryptionWriter`), avoid wrapping with those two middlewares or handle it in an outer layer yourself, so those capabilities don't silently stop working.

## Usage

The `httpx.With*` middlewares already use the wrappers above internally (the core logic lives in the `httpx/middleware` subpackage, see [httpx](./httpx.md)), so business code normally doesn't need them directly; if you need to wrap a ResponseWriter yourself (e.g. for custom log statistics), use this package directly.
