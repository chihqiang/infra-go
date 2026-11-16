package response

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedWriter_StateAndWrites(t *testing.T) {
	b := NewBufferedWriter()
	assert.Equal(t, http.StatusOK, b.Status())
	_, written := b.WrittenAt()
	assert.False(t, written)
	assert.False(t, b.Written())

	b.Header().Set("X-Test", "1")
	b.WriteHeader(http.StatusCreated)
	b.WriteHeader(http.StatusInternalServerError) // first status wins
	_, _ = b.Write([]byte("body"))

	at, written := b.WrittenAt()
	assert.True(t, written)
	assert.False(t, at.IsZero())
	assert.True(t, b.Written())
	assert.Equal(t, http.StatusCreated, b.Status())

	assert.ErrorIs(t, b.FlushError(), http.ErrHandlerTimeout)
	_, _, err := b.Hijack()
	assert.ErrorIs(t, err, http.ErrNotSupported)

	recorder := httptest.NewRecorder()
	require.NoError(t, b.WriteTo(recorder))
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "body", recorder.Body.String())
	assert.Equal(t, "1", recorder.Header().Get("X-Test"))
}

func TestBufferedWriter_WriteDefaultsStatus(t *testing.T) {
	b := NewBufferedWriter()
	_, _ = b.Write([]byte("hi"))
	assert.Equal(t, http.StatusOK, b.Status())
	assert.True(t, b.Written())
}

type shortResponseWriter struct {
	header http.Header
	status int
}

func (w *shortResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *shortResponseWriter) WriteHeader(status int) { w.status = status }
func (w *shortResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestBufferedWriter_WriteToShortWrite(t *testing.T) {
	b := NewBufferedWriter()
	_, _ = b.Write([]byte("body"))

	err := b.WriteTo(&shortResponseWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "short write")
}

func TestDeferredWriter_Flush(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := NewDeferredWriter(recorder)

	w.Header().Set("X-Test", "1")
	w.WriteHeader(http.StatusAccepted)
	w.WriteHeader(http.StatusInternalServerError) // ignored
	_, _ = w.Write([]byte("body"))
	w.Flush(http.StatusOK)
	w.Flush(http.StatusTeapot) // extra flush is a no-op

	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "body", recorder.Body.String())

	// After Flush, writes bypass the buffer.
	_, _ = w.Write([]byte("-late"))
	assert.Equal(t, "body-late", recorder.Body.String())
}

func TestDeferredWriter_FallbackStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := NewDeferredWriter(recorder)
	_, _ = w.Write([]byte("body"))
	w.Flush(http.StatusNoContent)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "body", recorder.Body.String())
}

func TestStatusWriter_DefaultsAndHijack(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := NewStatusWriter(recorder)
	assert.Equal(t, http.StatusOK, w.Status())

	w.WriteHeader(http.StatusBadRequest)
	w.WriteHeader(http.StatusInternalServerError) // ignored
	_, _ = w.Write([]byte("ok"))
	assert.Equal(t, http.StatusBadRequest, w.Status())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	_, _, err := w.Hijack()
	require.Error(t, err)
	assert.True(t, errors.Is(err, http.ErrNotSupported) || err != nil)
}

func TestStatusWriter_Flush(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := NewStatusWriter(recorder)
	assert.NotPanics(t, w.Flush)
}

func TestBufferedWriter_StatusZero(t *testing.T) {
	b := NewBufferedWriter()
	assert.Equal(t, http.StatusOK, b.statusOrDefault())
	assert.Equal(t, http.StatusOK, b.Status())
}

func TestWrittenAtTimestampsOrder(t *testing.T) {
	b := NewBufferedWriter()
	before := time.Now()
	_, _ = b.Write([]byte("x"))
	at, ok := b.WrittenAt()
	require.True(t, ok)
	assert.False(t, at.Before(before))
}
