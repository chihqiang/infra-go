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

// --- Flush 与状态码 ---

// countingWriter 记录 WriteHeader 被转发的次数，用于验证响应头不会被重复写出。
type countingWriter struct {
	*httptest.ResponseRecorder
	headerWrites int
}

func (w *countingWriter) WriteHeader(code int) {
	w.headerWrites++
	w.ResponseRecorder.WriteHeader(code)
}

// TestTimeoutWriter_FlushWritesExplicitStatusCode 验证 Flush 会把 handler 显式
// 设置的状态码写到底层，而不是被 net/http 的隐式 200 覆盖（历史缺陷）。
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

// TestTimeoutWriter_FlushThenDoneWritesHeaderOnce 验证 Flush 之后 Done 不会重复
// 写响应头（避免 "superfluous WriteHeader"）且状态码保持不变。
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

// TestTimeoutWriter_FlushWithOKStatus 验证 200 仍交由 net/http 隐式写入（不显式 WriteHeader）。
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

// TestTimeoutWriter_FlushAfterTimeoutNoop 验证超时后 Flush 不再向底层写入。
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
