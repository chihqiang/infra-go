package respw

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"sync"
)

// TimeoutWriter 缓存 handler 写入的响应，支持超时后的安全丢弃。
// 供 httpx.WithTimeout 中间件使用，实现 http.Flusher / http.Hijacker，
// 兼容流式与 WebSocket 等场景。
type TimeoutWriter struct {
	w    http.ResponseWriter
	h    http.Header
	wbuf bytes.Buffer

	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

// NewTimeoutWriter 创建一个超时响应包装器，缓存写入直到 Done/Flush。
func NewTimeoutWriter(w http.ResponseWriter) *TimeoutWriter {
	return &TimeoutWriter{w: w, h: make(http.Header), code: http.StatusOK}
}

// Header 返回临时响应头。
func (tw *TimeoutWriter) Header() http.Header { return tw.h }

// Write 写入响应体（先缓冲，超时后丢弃）。
func (tw *TimeoutWriter) Write(p []byte) (int, error) {
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
func (tw *TimeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.wroteHeader {
		tw.writeHeaderLocked(code)
	}
}

// Done 将缓冲的响应（响应头/状态码/响应体）一次性写到底层 ResponseWriter。
// 供 handler 正常结束（未超时）后调用。
func (tw *TimeoutWriter) Done() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	dst := tw.w.Header()
	for k, vv := range tw.h {
		dst[k] = vv
	}
	if tw.code != http.StatusOK {
		tw.w.WriteHeader(tw.code)
	}
	_, _ = tw.w.Write(tw.wbuf.Bytes())
}

// Timeout 标记请求已超时，使此后的 Write/WriteHeader 失效
// （Write 返回 http.ErrHandlerTimeout）。
func (tw *TimeoutWriter) Timeout() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.timedOut = true
}

// Flush 立即刷新缓冲到客户端（支持流式响应）。
func (tw *TimeoutWriter) Flush() {
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
func (tw *TimeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacked, ok := tw.w.(http.Hijacker); ok {
		return hijacked.Hijack()
	}
	return nil, nil, errors.New("respw: server doesn't support hijacking")
}

// writeHeaderLocked 在持锁状态下写入状态码。
func (tw *TimeoutWriter) writeHeaderLocked(code int) {
	tw.code = code
	tw.wroteHeader = true
}
