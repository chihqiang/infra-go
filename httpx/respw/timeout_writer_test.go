package respw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestTimeoutWriter(w http.ResponseWriter) *TimeoutWriter {
	return &TimeoutWriter{
		w:    w,
		h:    make(http.Header),
		code: http.StatusOK,
	}
}

func TestTimeoutWriter_Header(t *testing.T) {
	tw := newTestTimeoutWriter(httptest.NewRecorder())
	tw.h.Set("X-Test", "1")
	assert.Equal(t, "1", tw.Header().Get("X-Test"))
}

func TestTimeoutWriter_Write(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	n, err := tw.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, tw.code)
	assert.True(t, tw.wroteHeader)
	assert.Equal(t, "hello", tw.wbuf.String())
}

func TestTimeoutWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	tw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, tw.code)
	assert.True(t, tw.wroteHeader)
}

func TestTimeoutWriter_WriteAfterTimeout(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	// After a timeout, Write must return ErrHandlerTimeout and write nothing to the buffer
	tw.Timeout()
	_, err := tw.Write([]byte("x"))
	assert.ErrorIs(t, err, http.ErrHandlerTimeout)
	assert.Empty(t, tw.wbuf.Bytes())
}

func TestTimeoutWriter_DoneWritesBuffered(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	_, _ = tw.Write([]byte("data"))
	tw.h.Set("X-Test", "1")
	tw.Done()
	assert.Equal(t, "data", w.Body.String())
	assert.Equal(t, "1", w.Header().Get("X-Test"))
}

func TestTimeoutWriter_FlushWritesBuffer(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	_, _ = tw.Write([]byte("data"))
	tw.Flush()
	assert.True(t, w.Flushed)
	assert.Equal(t, "data", w.Body.String())
	// The buffer is cleared after flushing
	assert.Empty(t, tw.wbuf.Bytes())
}

func TestTimeoutWriter_HijackUnsupported(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	_, _, err := tw.Hijack()
	assert.Error(t, err)
}

// --- Flush and status code ---

// countingWriter records how often WriteHeader is forwarded, verifying that
// response headers are never written twice.
type countingWriter struct {
	*httptest.ResponseRecorder
	headerWrites int
}

func (w *countingWriter) WriteHeader(code int) {
	w.headerWrites++
	w.ResponseRecorder.WriteHeader(code)
}

// TestTimeoutWriter_FlushWritesExplicitStatusCode verifies that Flush writes the
// status code explicitly set by the handler to the underlying writer instead of
// letting net/http's implicit 200 override it (historical defect).
func TestTimeoutWriter_FlushWritesExplicitStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	tw := newTestTimeoutWriter(rec)

	tw.WriteHeader(http.StatusCreated)
	_, _ = tw.Write([]byte("created"))
	tw.Flush()

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "created", rec.Body.String())
	assert.True(t, rec.Flushed)
}

// TestTimeoutWriter_FlushThenDoneWritesHeaderOnce verifies that Done after Flush
// does not write the response header twice (avoiding "superfluous WriteHeader")
// and that the status code stays unchanged.
func TestTimeoutWriter_FlushThenDoneWritesHeaderOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseRecorder: rec}
	tw := newTestTimeoutWriter(cw)

	tw.WriteHeader(http.StatusPartialContent)
	_, _ = tw.Write([]byte("a"))
	tw.Flush()
	_, _ = tw.Write([]byte("b"))
	tw.Done()

	assert.Equal(t, 1, cw.headerWrites, "WriteHeader should be forwarded exactly once")
	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "ab", rec.Body.String())
}

// TestTimeoutWriter_FlushWithOKStatus verifies that 200 is still left to net/http
// to write implicitly (no explicit WriteHeader).
func TestTimeoutWriter_FlushWithOKStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseRecorder: rec}
	tw := newTestTimeoutWriter(cw)

	_, _ = tw.Write([]byte("ok"))
	tw.Flush()

	assert.Equal(t, 0, cw.headerWrites)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

// TestTimeoutWriter_FlushAfterTimeoutNoop verifies that Flush no longer writes to
// the underlying writer after a timeout.
func TestTimeoutWriter_FlushAfterTimeoutNoop(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseRecorder: rec}
	tw := newTestTimeoutWriter(cw)

	tw.Timeout()
	tw.Flush()

	assert.Equal(t, 0, cw.headerWrites)
	assert.Empty(t, rec.Body.String())
	assert.False(t, rec.Flushed)
}
