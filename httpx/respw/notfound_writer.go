package respw

import "net/http"

// NotFoundResponseWriter 拦截底层 ResponseWriter 写入的 404 响应，
// 将其转交给自定义处理器，避免 ServeMux 写入默认的 "404 page not found"。
// 供 httpx.Server 的 SetNotFoundHandler 使用：包装 ServeMux 的 ResponseWriter，
// 当底层尝试写入 404 状态码时，转由 handler 处理；handler 完成后，
// 后续的 404 正文写入会被吞掉，避免覆盖自定义响应。
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
