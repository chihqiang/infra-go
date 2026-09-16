package respw

import (
	"bytes"
	"net/http"
)

// CryptionWriter buffers the response written by a handler so it can be
// encrypted as a whole before being sent out.
// It is used by the httpx.WithCryption middleware: the handler response is
// buffered in memory first, and the middleware encrypts it with AES-GCM and
// writes it out once the handler finishes.
// maxBufBytes caps the buffer size; once it is exceeded (Overflowed is true),
// the writer switches to "plaintext direct write" mode: the buffered content is
// written out as-is first, then all later writes are passed straight through,
// so the response is never truncated (which would corrupt the data).
type CryptionWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	code        int  // records the status code; deferred in buffered mode, written after encryption
	overflowed  bool // whether the buffer limit was exceeded (plaintext passthrough after that)
	wroteHeader bool // whether the status code was sent to the underlying writer (once in passthrough mode)
	maxBufBytes int  // maximum number of buffered bytes
}

// NewCryptionWriter creates a buffering wrapper used to encrypt responses.
// maxBufBytes is the buffer limit; <=0 means no limit.
func NewCryptionWriter(w http.ResponseWriter, maxBufBytes int) *CryptionWriter {
	return &CryptionWriter{ResponseWriter: w, maxBufBytes: maxBufBytes}
}

// Overflowed reports whether the buffer limit has been exceeded.
// After that, writes go straight to the underlying writer as plaintext, so the
// middleware must not emit the buffered content again.
func (w *CryptionWriter) Overflowed() bool { return w.overflowed }

// StatusCode returns the status code recorded by the handler
// (0 when WriteHeader was never called explicitly).
func (w *CryptionWriter) StatusCode() int { return w.code }

// Buffered returns the buffered response body.
func (w *CryptionWriter) Buffered() []byte { return w.buf.Bytes() }

// Header returns the response headers of the underlying ResponseWriter.
func (w *CryptionWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// Write buffers the response body in memory; it is encrypted and written out as
// a whole once the handler finishes.
//
// Once maxBufBytes is exceeded the writer switches to plaintext passthrough
// mode: the buffered content is written to the underlying writer as-is first,
// then this write and all later ones go straight through, so the client always
// receives the complete response.
// (Historical defect: after overflow only the truncated buffer prefix was kept
// and emitted, silently dropping the rest of the response body.)
func (w *CryptionWriter) Write(p []byte) (int, error) {
	if w.overflowed {
		return w.ResponseWriter.Write(p)
	}
	if w.maxBufBytes > 0 && w.buf.Len()+len(p) > w.maxBufBytes {
		w.overflowed = true
		w.writeHeaderToUnderlying()
		if w.buf.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.buf.Bytes()); err != nil {
				w.buf.Reset()
				return 0, err
			}
			w.buf.Reset()
		}
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

// WriteHeader records the status code; in buffered mode it is deferred and
// written to the underlying writer once encryption/passthrough is done, while in
// plaintext passthrough mode it is forwarded to the underlying writer at once.
func (w *CryptionWriter) WriteHeader(code int) {
	w.code = code
	if w.overflowed {
		w.writeHeaderToUnderlying()
	}
}

// writeHeaderToUnderlying writes the recorded status code to the underlying
// ResponseWriter (first call only).
// When code is 0 (the handler never called WriteHeader explicitly) nothing is
// written and net/http implicitly sends 200.
func (w *CryptionWriter) writeHeaderToUnderlying() {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if w.code != 0 {
		w.ResponseWriter.WriteHeader(w.code)
	}
}

// Flush forwards to the underlying Flush in plaintext passthrough mode (needed
// to stream large responses); in buffered mode it is a no-op, because encryption
// needs the complete response body before writing anything out, and flushing
// early would send an empty response while the data is not ready yet, leaving the
// response "half sent".
func (w *CryptionWriter) Flush() {
	if !w.overflowed {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
