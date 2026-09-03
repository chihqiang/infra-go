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

	// 标记超时后，Write 应返回 ErrHandlerTimeout，且不写入缓冲
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
	// 刷新后缓冲被清空
	assert.Empty(t, tw.wbuf.Bytes())
}

func TestTimeoutWriter_HijackUnsupported(t *testing.T) {
	w := httptest.NewRecorder()
	tw := newTestTimeoutWriter(w)

	_, _, err := tw.Hijack()
	assert.Error(t, err)
}
