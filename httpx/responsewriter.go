package httpx

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"sync"
)

// statusRecorder 包装 http.ResponseWriter，捕获状态码和响应字节数。
// 供 WithLogger 等内部中间件使用。默认状态码为 200（handler 未显式调用 WriteHeader 时）。
//
// 注意：正确透传 Unwrap/Flush/Hijack/Push 可选接口，避免包装导致
// SSE 流式、WebSocket 升级、HTTP/2 Push 等功能静默失效。
type statusRecorder struct {
	http.ResponseWriter
	status    int
	bytes     int
	wroteHead bool
}

// newStatusRecorder 创建一个响应记录器，包装给定的 http.ResponseWriter。
func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader 记录状态码（仅首次有效），并委托给底层 ResponseWriter。
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write 累计写入字节数，并委托给底层 ResponseWriter。
func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap 返回底层 ResponseWriter，供 http.ResponseController 使用。
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush 将缓冲数据刷新到客户端（SSE 等流式响应需要）。
// 底层 ResponseWriter 不支持 Flush 时静默忽略。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 劫持底层连接（WebSocket 升级等场景需要）。
// 底层 ResponseWriter 不支持 Hijack 时返回错误。
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("httpx: hijacking not supported")
}

// Push 发起 HTTP/2 Server Push。
// 底层 ResponseWriter 不支持 Push 时返回错误。
func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if p, ok := r.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return errors.New("httpx: push not supported")
}

// --- timeoutWriter ---

// timeoutWriter 缓存 handler 写入的响应，支持超时后的安全丢弃。
// 供 WithTimeout 中间件使用，实现 http.Flusher / http.Hijacker，
// 兼容流式与 WebSocket 等场景。
type timeoutWriter struct {
	w    http.ResponseWriter
	h    http.Header
	wbuf bytes.Buffer

	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

// Header 返回临时响应头。
func (tw *timeoutWriter) Header() http.Header { return tw.h }

// Write 写入响应体（先缓冲，超时后丢弃）。
func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !tw.wroteHeader {
		tw.writeHeaderLocked(http.StatusOK)
	}
	return tw.wbuf.Write(p)
}

// WriteHeader 写入状态码（超时后忽略后续写入）。
func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.wroteHeader {
		tw.writeHeaderLocked(code)
	}
}

// Flush 立即刷新缓冲到客户端（支持流式响应）。
func (tw *timeoutWriter) Flush() {
	flusher, ok := tw.w.(http.Flusher)
	if !ok {
		return
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	header := tw.w.Header()
	for k, v := range tw.h {
		header[k] = v
	}
	_, _ = tw.w.Write(tw.wbuf.Bytes())
	tw.wbuf.Reset()
	flusher.Flush()
}

// Hijack 支持 WebSocket 升级等底层连接接管场景。
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacked, ok := tw.w.(http.Hijacker); ok {
		return hijacked.Hijack()
	}
	return nil, nil, errors.New("httpx: server doesn't support hijacking")
}

// writeHeaderLocked 在持锁状态下写入状态码。
func (tw *timeoutWriter) writeHeaderLocked(code int) {
	tw.code = code
	tw.wroteHeader = true
}

// --- cryptionResponseWriter ---

// cryptionResponseWriter 缓冲 handler 写入的响应，便于整体加密后输出。
// 供 WithCryption 中间件使用：handler 的响应先写入内存缓冲，
// handler 结束后由中间件统一 AES-GCM 加密后输出。
// maxBufBytes 限制缓冲上限，超出时 subsequent Write 返回错误，
// 中间件检测到 overflow 后回退为明文输出，避免 OOM。
type cryptionResponseWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	code        int  // 记录状态码；Write 时不透传，加密完成后统一写
	overflowed  bool // 缓冲是否超限
	maxBufBytes int  // 最大缓冲字节数
}

// Header 返回底层 ResponseWriter 的响应头。
func (w *cryptionResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// Write 将响应体缓冲到内存，待 handler 结束后统一加密输出。
// 超过 maxBufBytes 时标记 overflowed 并拒绝继续写入，
// 中间件将回退为明文输出。
func (w *cryptionResponseWriter) Write(p []byte) (int, error) {
	if w.overflowed {
		return 0, errors.New("httpx: encrypted response buffer overflowed")
	}
	if w.maxBufBytes > 0 && w.buf.Len()+len(p) > w.maxBufBytes {
		w.overflowed = true
		return 0, errors.New("httpx: encrypted response exceeds max buffer size")
	}
	return w.buf.Write(p)
}

// WriteHeader 记录状态码（延迟到加密完成后写入）。
func (w *cryptionResponseWriter) WriteHeader(code int) {
	w.code = code
}

// Flush 底层支持 Flush 时透传（流式响应场景）。
func (w *cryptionResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
