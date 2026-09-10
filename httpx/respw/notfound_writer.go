package respw

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// NotFoundResponseWriter 拦截底层 ResponseWriter 写入的 404 响应，
// 将其转交给自定义处理器，避免 ServeMux 写入默认的 "404 page not found"。
// 供 httpx.Server 的 SetNotFoundHandler 使用：包装 ServeMux 的 ResponseWriter，
// 当底层尝试写入 404 状态码时，转由 handler 处理；handler 完成后，
// 后续的 404 正文写入会被吞掉，避免覆盖自定义响应。
//
// 使用建议：该类型无法区分「路由未匹配」与「业务主动返回 404」，
// 因此会一并劫持业务 404。若目标只是替换 ServeMux 的默认 404 页面，
// 推荐在调用前用 ServeMux.Handler(r) 预判路由是否命中
// （httpx.Server 内部即采用该方式），而不是包装 ResponseWriter。
//
// 为避免破坏 SSE / WebSocket / HTTP/2 等场景，本类型透传
// Flush / Hijack / Push / Unwrap 等可选接口。
type NotFoundResponseWriter struct {
	http.ResponseWriter
	request    *http.Request
	handler    http.HandlerFunc
	handled    bool // 是否已转交给自定义 404 处理器
	suppressed bool // 是否吞掉后续写入
}

// NewNotFoundResponseWriter 创建 404 拦截包装器。
// request 为当前请求，handler 为收到 404 时转交的自定义处理器。
func NewNotFoundResponseWriter(w http.ResponseWriter, request *http.Request, handler http.HandlerFunc) *NotFoundResponseWriter {
	return &NotFoundResponseWriter{
		ResponseWriter: w,
		request:        request,
		handler:        handler,
	}
}

// WriteHeader 拦截 404 状态码，转交给自定义处理器。
func (w *NotFoundResponseWriter) WriteHeader(status int) {
	if status == http.StatusNotFound && !w.handled {
		w.handled = true
		w.suppressed = true
		w.handler(w.ResponseWriter, w.request)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write 在已转交自定义处理器后吞掉底层 404 正文。
func (w *NotFoundResponseWriter) Write(p []byte) (int, error) {
	if w.suppressed {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

// Flush 透传底层 ResponseWriter 的 Flush 能力（SSE / 流式响应）。
func (w *NotFoundResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传底层 ResponseWriter 的 Hijack 能力（WebSocket 升级）。
func (w *NotFoundResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("respw: server doesn't support hijacking")
}

// Push 透传底层 ResponseWriter 的 HTTP/2 Push 能力。
func (w *NotFoundResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap 返回底层 ResponseWriter，供 http.ResponseController 使用。
func (w *NotFoundResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
