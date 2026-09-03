// Package respw 提供对 http.ResponseWriter 的增强包装工具。
//
// 当前提供 RecorderWriter，用于透明包装 ResponseWriter 并捕获响应状态码与
// 写入字节数，供 httpx（访问日志 / 熔断）、trace（记录 span 状态码）等中间件
// 复用，避免各包重复实现导致能力漂移（如漏透传 Flush/Hijack 等可选接口）。
package respw

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// RecorderWriter 包装 http.ResponseWriter，捕获状态码和写入字节数。
// 默认状态码为 200（handler 未显式调用 WriteHeader 时）。
//
// 注意：正确透传 Unwrap/Flush/Hijack/Push 可选接口，避免包装导致
// SSE 流式、WebSocket 升级、HTTP/2 Push 等功能静默失效。
type RecorderWriter struct {
	http.ResponseWriter
	status    int
	bytes     int
	wroteHead bool
}

// NewRecorderWriter 创建一个响应记录器，包装给定的 http.ResponseWriter。
func NewRecorderWriter(w http.ResponseWriter) *RecorderWriter {
	return &RecorderWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status 返回实际响应状态码（未显式 WriteHeader 时为 200）。
func (r *RecorderWriter) Status() int { return r.status }

// Bytes 返回累计写入的响应字节数。
func (r *RecorderWriter) Bytes() int { return r.bytes }

// WriteHeader 记录状态码（仅首次有效），并委托给底层 ResponseWriter。
func (r *RecorderWriter) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write 累计写入字节数，并委托给底层 ResponseWriter。
func (r *RecorderWriter) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap 返回底层 ResponseWriter，供 http.ResponseController 使用。
func (r *RecorderWriter) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush 将缓冲数据刷新到客户端（SSE 等流式响应需要）。
// 底层 ResponseWriter 不支持 Flush 时静默忽略。
func (r *RecorderWriter) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 劫持底层连接（WebSocket 升级等场景需要）。
// 底层 ResponseWriter 不支持 Hijack 时返回错误。
func (r *RecorderWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("respw: hijacking not supported")
}

// Push 发起 HTTP/2 Server Push。
// 底层 ResponseWriter 不支持 Push 时返回错误。
func (r *RecorderWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := r.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return errors.New("respw: push not supported")
}
