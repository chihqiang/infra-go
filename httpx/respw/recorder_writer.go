// Package respw provides enhanced wrappers around http.ResponseWriter.
//
// It currently offers RecorderWriter, which transparently wraps a
// ResponseWriter and captures the response status code and the number of bytes
// written, so that httpx/middleware components (access logs / circuit breakers /
// tracing, which records span status codes) can reuse it instead of each package
// implementing its own drifting version (for example forgetting to forward
// optional interfaces such as Flush/Hijack).
package respw

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// RecorderWriter wraps an http.ResponseWriter and captures the status code and
// the number of bytes written. The default status code is 200 (when the handler
// never calls WriteHeader explicitly).
//
// Note: the optional Unwrap/Flush/Hijack/Push interfaces are forwarded correctly,
// so wrapping does not silently break features such as SSE streaming, WebSocket
// upgrade or HTTP/2 Push.
type RecorderWriter struct {
	http.ResponseWriter
	status    int
	bytes     int
	wroteHead bool
}

// NewRecorderWriter creates a response recorder wrapping the given http.ResponseWriter.
func NewRecorderWriter(w http.ResponseWriter) *RecorderWriter {
	return &RecorderWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status returns the actual response status code (200 when WriteHeader was not called).
func (r *RecorderWriter) Status() int { return r.status }

// Bytes returns the total number of response bytes written.
func (r *RecorderWriter) Bytes() int { return r.bytes }

// WriteHeader records the status code (first call only) and delegates to the
// underlying ResponseWriter.
func (r *RecorderWriter) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write accumulates the number of bytes written and delegates to the underlying
// ResponseWriter.
func (r *RecorderWriter) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (r *RecorderWriter) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush flushes buffered data to the client (required by streaming responses
// such as SSE). It is silently ignored when the underlying ResponseWriter does
// not support Flush.
func (r *RecorderWriter) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hijacks the underlying connection (required for scenarios such as
// WebSocket upgrade). It returns an error when the underlying ResponseWriter
// does not support Hijack.
func (r *RecorderWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("respw: hijacking not supported")
}

// Push performs an HTTP/2 Server Push. It returns an error when the underlying
// ResponseWriter does not support Push.
func (r *RecorderWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := r.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return errors.New("respw: push not supported")
}
