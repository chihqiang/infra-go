package response

import (
	"bytes"
	"net/http"
)

// DeferredWriter buffers the body so the final status can be chosen after the
// handler runs. A status written by the handler takes precedence over the
// default passed to Flush.
type DeferredWriter struct {
	http.ResponseWriter
	status    int
	body      bytes.Buffer
	committed bool
}

// NewDeferredWriter returns a writer that buffers all output.
func NewDeferredWriter(w http.ResponseWriter) *DeferredWriter {
	return &DeferredWriter{ResponseWriter: w}
}

// WriteHeader records status instead of sending it immediately.
func (w *DeferredWriter) WriteHeader(status int) {
	if w.status != 0 || w.committed {
		return
	}
	w.status = status
}

// Write buffers p until Flush is called.
func (w *DeferredWriter) Write(p []byte) (int, error) {
	if w.committed {
		return w.ResponseWriter.Write(p)
	}
	return w.body.Write(p)
}

// Flush sends the buffered response using the explicit status, or fallback when
// the handler did not set one. Extra flushes are no-ops.
func (w *DeferredWriter) Flush(fallback int) {
	if w.committed {
		return
	}
	w.committed = true

	status := w.status
	if status == 0 {
		status = fallback
	}
	w.ResponseWriter.WriteHeader(status)
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *DeferredWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
