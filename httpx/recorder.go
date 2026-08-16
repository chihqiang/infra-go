package httpx

import (
	"bufio"
	"errors"
	"net"
	"net/http"
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
