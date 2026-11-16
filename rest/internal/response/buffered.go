package response

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// BufferedWriter captures a response so it can be discarded or replayed later.
// It is safe for concurrent use.
type BufferedWriter struct {
	mu        sync.Mutex
	header    http.Header
	status    int
	body      bytes.Buffer
	written   bool
	writtenAt time.Time
}

// NewBufferedWriter returns an empty buffered writer.
func NewBufferedWriter() *BufferedWriter {
	return &BufferedWriter{header: make(http.Header)}
}

// Header returns the buffered response headers.
func (b *BufferedWriter) Header() http.Header {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.header
}

// WriteHeader records the first final status written. Informational (1xx)
// responses cannot be replayed before the final response, so they are dropped
// instead of consuming the status slot.
func (b *BufferedWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.written {
		return
	}
	b.written = true
	b.writtenAt = time.Now()
	b.status = status
}

// Write buffers p, defaulting the status to 200.
func (b *BufferedWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.written {
		b.written = true
		b.writtenAt = time.Now()
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// Written reports whether the handler wrote a status or a body.
func (b *BufferedWriter) Written() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

// WrittenAt reports when the handler first wrote and whether it wrote at all.
// It lets the timeout middleware tell a response finished before the deadline
// from a late write produced after the handler's context was cancelled.
func (b *BufferedWriter) WrittenAt() (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writtenAt, b.written
}

// Status returns the recorded status, or 200 when nothing was written.
func (b *BufferedWriter) Status() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.statusOrDefault()
}

// WriteTo copies the buffered response to w.
func (b *BufferedWriter) WriteTo(w http.ResponseWriter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	target := w.Header()
	for key, values := range b.header {
		target.Del(key)
		for _, value := range values {
			target.Add(key, value)
		}
	}

	w.WriteHeader(b.statusOrDefault())
	if b.body.Len() == 0 {
		return nil
	}
	written, err := w.Write(b.body.Bytes())
	if err != nil {
		return err
	}
	if written < b.body.Len() {
		return fmt.Errorf("response: short write: wrote %d of %d bytes", written, b.body.Len())
	}
	return nil
}

// FlushError reports that streaming is impossible while the response is buffered
// for a possible timeout: flushing would commit headers before the timeout can
// answer 503. Callers get the same sentinel as net/http's TimeoutHandler instead
// of the generic "feature not supported".
func (b *BufferedWriter) FlushError() error {
	return http.ErrHandlerTimeout
}

// Hijack refuses protocol upgrades (WebSocket, CONNECT) for the same reason: the
// connection cannot be handed over while the response is buffered.
func (b *BufferedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

func (b *BufferedWriter) statusOrDefault() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}
