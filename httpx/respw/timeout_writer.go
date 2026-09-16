package respw

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"sync"
)

// TimeoutWriter buffers the response written by a handler so it can be safely
// discarded after a timeout. It is used by the httpx.WithTimeout middleware and
// implements http.Flusher / http.Hijacker, keeping streaming and WebSocket
// scenarios working.
type TimeoutWriter struct {
	w    http.ResponseWriter
	h    http.Header
	wbuf bytes.Buffer

	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	headerSent  bool // whether the status code/headers were written to the underlying ResponseWriter
	code        int
}

// NewTimeoutWriter creates a timeout response wrapper that buffers writes until
// Done/Flush.
func NewTimeoutWriter(w http.ResponseWriter) *TimeoutWriter {
	return &TimeoutWriter{w: w, h: make(http.Header), code: http.StatusOK}
}

// Header returns the temporary response headers.
func (tw *TimeoutWriter) Header() http.Header { return tw.h }

// Write writes the response body (buffered first, discarded after a timeout).
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

// WriteHeader writes the status code (later writes are ignored after a timeout).
func (tw *TimeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.wroteHeader {
		tw.writeHeaderLocked(code)
	}
}

// Done writes the buffered response (headers/status code/body) to the underlying
// ResponseWriter in one go. It is meant to be called after the handler finishes
// normally (without timing out).
func (tw *TimeoutWriter) Done() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.writeHeaderToUnderlyingLocked()
	_, _ = tw.w.Write(tw.wbuf.Bytes())
	tw.wbuf.Reset()
}

// Timeout marks the request as timed out, disabling subsequent Write/WriteHeader
// calls (Write returns http.ErrHandlerTimeout).
func (tw *TimeoutWriter) Timeout() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.timedOut = true
}

// Flush immediately flushes the buffer to the client (supports streaming responses).
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
	// The headers and status code must be written to the underlying writer first:
	// net/http implicitly writes 200 OK on the first Write, so if the status code
	// explicitly set by the handler (such as 201/206/500) is not written here, the
	// client would always see 200 and a later status code written by Done would be
	// ignored.
	tw.writeHeaderToUnderlyingLocked()
	_, _ = tw.w.Write(tw.wbuf.Bytes())
	tw.wbuf.Reset()
	flusher.Flush()
}

// Hijack supports underlying connection takeover scenarios such as WebSocket
// upgrade.
func (tw *TimeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacked, ok := tw.w.(http.Hijacker); ok {
		return hijacked.Hijack()
	}
	return nil, nil, errors.New("respw: server doesn't support hijacking")
}

// writeHeaderToUnderlyingLocked writes the buffered response headers and status
// code to the underlying ResponseWriter while holding the lock.
//
// Only the first call has an effect: calling Done after Flush does not repeat
// WriteHeader (avoiding "superfluous WriteHeader" and not overriding the status
// code that was already sent). When the status code is 200, WriteHeader is not
// called explicitly and net/http implicitly writes it on the first Write.
func (tw *TimeoutWriter) writeHeaderToUnderlyingLocked() {
	if tw.headerSent {
		return
	}
	tw.headerSent = true
	dst := tw.w.Header()
	for k, vv := range tw.h {
		dst[k] = vv
	}
	if tw.code != http.StatusOK {
		tw.w.WriteHeader(tw.code)
	}
}

// writeHeaderLocked records the status code while holding the lock.
func (tw *TimeoutWriter) writeHeaderLocked(code int) {
	tw.code = code
	tw.wroteHeader = true
}
