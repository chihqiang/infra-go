package respw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCryptionWriter_WriteBuffers(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &CryptionWriter{ResponseWriter: w}

	n, err := cw.Write([]byte("secret data"))
	assert.NoError(t, err)
	assert.Equal(t, 11, n)
	// Written to the buffer rather than the underlying writer (awaiting encryption)
	assert.Equal(t, "secret data", string(cw.Buffered()))
	assert.Empty(t, w.Body.String())
}

func TestCryptionWriter_WriteHeaderRecords(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &CryptionWriter{ResponseWriter: w}

	cw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, cw.StatusCode())
}

func TestCryptionWriter_HeaderDelegates(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &CryptionWriter{ResponseWriter: w}

	cw.Header().Set("X-Test", "1")
	assert.Equal(t, "1", w.Header().Get("X-Test"))
}

func TestCryptionWriter_Overflow(t *testing.T) {
	w := httptest.NewRecorder()
	cw := NewCryptionWriter(w, 4) // buffer limit of 4 bytes

	_, err := cw.Write([]byte("abc"))
	assert.NoError(t, err)
	assert.False(t, cw.Overflowed())
	assert.Empty(t, w.Body.String(), "buffered mode must not write to the underlying writer")

	// Over the limit: it switches to plaintext passthrough, so both the buffered
	// content and this write must reach the underlying writer as-is
	n, err := cw.Write([]byte("def"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.True(t, cw.Overflowed())
	assert.Equal(t, "abcdef", w.Body.String(), "buffered and subsequent content must all be written out")
	assert.Empty(t, cw.Buffered())

	// After switching to passthrough mode, further writes are still written out in full
	_, err = cw.Write([]byte("ghi"))
	assert.NoError(t, err)
	assert.Equal(t, "abcdefghi", w.Body.String())
}

func TestCryptionWriter_OverflowWritesStatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	cw := NewCryptionWriter(w, 2)

	cw.WriteHeader(http.StatusCreated)
	_, _ = cw.Write([]byte("ab"))
	_, _ = cw.Write([]byte("cd")) // triggers passthrough

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "abcd", w.Body.String())
}

func TestCryptionWriter_OverflowFlushes(t *testing.T) {
	// In passthrough mode Flush must reach the underlying writer (so large responses
	// can be streamed)
	underlying := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	cw := NewCryptionWriter(underlying, 2)

	_, _ = cw.Write([]byte("ab"))
	_, _ = cw.Write([]byte("cd")) // triggers passthrough
	cw.Flush()

	assert.True(t, underlying.flushed)
	assert.Equal(t, "abcd", underlying.Body.String())
}

func TestCryptionWriter_FlushIsNoop(t *testing.T) {
	// Encryption needs the whole body buffered first, so Flush must not reach the
	// underlying writer (that would send a "half sent" empty response)
	underlying := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	cw := &CryptionWriter{ResponseWriter: underlying}

	cw.WriteHeader(http.StatusOK)
	cw.Flush()

	assert.False(t, underlying.flushed, "Flush must not reach underlying writer")
	assert.Empty(t, underlying.Body.String())
}

// flushRecorder is a test writer that records whether Flush was called.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() { f.flushed = true }
