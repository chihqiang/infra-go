package respw

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// NotFoundResponseWriter intercepts the 404 response written by the underlying
// ResponseWriter and hands it over to a custom handler, preventing ServeMux from
// writing its default "404 page not found".
// It is used by httpx.Server's SetNotFoundHandler: it wraps the ResponseWriter of
// a ServeMux, and when the underlying writer tries to write the 404 status code
// the custom handler takes over; once the handler finishes, later writes of the
// 404 body are swallowed so they cannot overwrite the custom response.
//
// Usage note: this type cannot tell "no route matched" apart from "the business
// logic deliberately returned 404", so business 404s are hijacked as well. If the
// only goal is to replace the default ServeMux 404 page, prefer checking whether
// a route matches with ServeMux.Handler(r) before the call (which is what
// httpx.Server does internally) instead of wrapping the ResponseWriter.
//
// To avoid breaking SSE / WebSocket / HTTP/2 and similar scenarios, this type
// forwards the optional interfaces Flush / Hijack / Push / Unwrap.
type NotFoundResponseWriter struct {
	http.ResponseWriter
	request    *http.Request
	handler    http.HandlerFunc
	handled    bool // whether the custom 404 handler has taken over
	suppressed bool // whether later writes are swallowed
}

// NewNotFoundResponseWriter creates a wrapper that intercepts 404 responses.
// request is the current request and handler is the custom handler that takes
// over when a 404 is received.
func NewNotFoundResponseWriter(w http.ResponseWriter, request *http.Request, handler http.HandlerFunc) *NotFoundResponseWriter {
	return &NotFoundResponseWriter{
		ResponseWriter: w,
		request:        request,
		handler:        handler,
	}
}

// WriteHeader intercepts the 404 status code and hands it to the custom handler.
func (w *NotFoundResponseWriter) WriteHeader(status int) {
	if status == http.StatusNotFound && !w.handled {
		w.handled = true
		w.suppressed = true
		w.handler(w.ResponseWriter, w.request)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write swallows the underlying 404 body once the custom handler has taken over.
func (w *NotFoundResponseWriter) Write(p []byte) (int, error) {
	if w.suppressed {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

// Flush forwards the Flush capability of the underlying ResponseWriter
// (SSE / streaming responses).
func (w *NotFoundResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards the Hijack capability of the underlying ResponseWriter
// (WebSocket upgrade).
func (w *NotFoundResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("respw: server doesn't support hijacking")
}

// Push forwards the HTTP/2 Push capability of the underlying ResponseWriter.
func (w *NotFoundResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (w *NotFoundResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
