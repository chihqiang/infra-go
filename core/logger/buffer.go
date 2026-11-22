package logger

import (
	"bufio"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	// flushSize is the buffer size at which a sink is flushed.
	flushSize = 64 << 10
	// flushInterval bounds how long an entry may stay buffered when there is
	// little traffic (see Logger.startFlusher).
	flushInterval = time.Second
)

// errSinkClosed is returned when a write arrives after the sink was closed.
var errSinkClosed = errors.New("logger: log sink is closed")

// bufferedWriter batches log entries into a bufio.Writer so one write syscall
// serves many entries. Each sink owns its buffer and mutex, so access logs do
// not contend with error logs.
type bufferedWriter struct {
	mu        sync.Mutex
	out       *bufio.Writer
	flushSize int
	closed    bool
}

// newBufferedWriter wraps out with a buffered writer.
func newBufferedWriter(out io.Writer, size int) *bufferedWriter {
	if size <= 0 {
		size = flushSize
	}
	return &bufferedWriter{out: bufio.NewWriterSize(out, size), flushSize: size}
}

// Write appends p and flushes once the buffer is full enough.
func (b *bufferedWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, errSinkClosed
	}
	n, err := b.out.Write(p)
	if err != nil {
		return n, err
	}
	if b.out.Buffered() >= b.flushSize {
		return n, b.out.Flush()
	}
	return n, nil
}

// Flush writes the buffered entries to the underlying writer.
func (b *bufferedWriter) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	return b.out.Flush()
}

// Close flushes and marks the sink closed. The underlying writer is closed by
// its owner, not here, so a shared os.Stdout is never closed.
func (b *bufferedWriter) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return b.out.Flush()
}
