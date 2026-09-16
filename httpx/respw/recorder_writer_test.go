package respw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecorderWriter_DefaultStatus(t *testing.T) {
	// The handler writes without calling WriteHeader, so status must be 200
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	n, err := rec.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, rec.Status(), "defaults to 200 when WriteHeader is not called")
	assert.Equal(t, 5, rec.Bytes())
}

func TestRecorderWriter_CustomStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	rec.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rec.Status())
	assert.True(t, rec.wroteHead)
}

func TestRecorderWriter_FirstStatusWins(t *testing.T) {
	// Only the first of several WriteHeader calls is recorded (per the HTTP spec)
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusTeapot, rec.Status())
}

func TestRecorderWriter_AccumulatesBytes(t *testing.T) {
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	_, _ = rec.Write([]byte("abc"))
	_, _ = rec.Write([]byte("defg"))
	assert.Equal(t, 7, rec.Bytes())
}

func TestRecorderWriter_Unwrap(t *testing.T) {
	// Unwrap must return the underlying ResponseWriter for http.ResponseController
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	assert.Same(t, w, rec.Unwrap())
}

func TestRecorderWriter_Flush(t *testing.T) {
	// Silently ignored when the underlying writer does not support Flush; never panics
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	rec.Flush()
	assert.True(t, w.Flushed)
}

func TestRecorderWriter_HijackUnsupported(t *testing.T) {
	// Returns an error when the underlying ResponseWriter does not support Hijack
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	_, _, err := rec.Hijack()
	assert.Error(t, err)
}

func TestRecorderWriter_PushUnsupported(t *testing.T) {
	// Returns an error when the underlying ResponseWriter does not support Push
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	err := rec.Push("/asset.js", nil)
	assert.Error(t, err)
}
