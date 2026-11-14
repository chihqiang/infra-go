package response

import (
	"bytes"
	"net/http"
	"sync"
)

// BufferedWriter captures a response so it can be discarded or replayed later.
// It is safe for concurrent use.
type BufferedWriter struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    bytes.Buffer
	written bool
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

// WriteHeader records the first status written.
func (b *BufferedWriter) WriteHeader(status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.written {
		return
	}
	b.written = true
	b.status = status
}

// Write buffers p, defaulting the status to 200.
func (b *BufferedWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.written {
		b.written = true
		b.status = http.StatusOK
	}
	return b.body.Write(p)
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
	if b.body.Len() > 0 {
		_, err := w.Write(b.body.Bytes())
		return err
	}
	return nil
}

func (b *BufferedWriter) statusOrDefault() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}
