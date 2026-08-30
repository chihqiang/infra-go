package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- statusRecorder 单元测试 ---

func TestStatusRecorder_DefaultStatus(t *testing.T) {
	// handler 不调 WriteHeader，直接 Write，status 应为 200
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	n, err := rec.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, rec.status, "未显式 WriteHeader 时默认 200")
	assert.Equal(t, 5, rec.bytes)
}

func TestStatusRecorder_CustomStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	rec.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rec.status)
	assert.True(t, rec.wroteHead)
}

func TestStatusRecorder_FirstStatusWins(t *testing.T) {
	// 多次 WriteHeader 只记录第一次（符合 HTTP 规范）
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusTeapot, rec.status)
}

func TestStatusRecorder_AccumulatesBytes(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	_, _ = rec.Write([]byte("abc"))
	_, _ = rec.Write([]byte("defg"))
	assert.Equal(t, 7, rec.bytes)
}

func TestStatusRecorder_Unwrap(t *testing.T) {
	// Unwrap 应返回底层 ResponseWriter，供 http.ResponseController 使用
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	assert.Same(t, w, rec.Unwrap())
}

func TestStatusRecorder_Flush(t *testing.T) {
	// 底层不支持 Flush 时静默忽略，不 panic
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	rec.Flush()
	assert.True(t, w.Flushed)
}

func TestStatusRecorder_HijackUnsupported(t *testing.T) {
	// 底层 ResponseWriter 不支持 Hijack 时返回错误
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	_, _, err := rec.Hijack()
	assert.Error(t, err)
}

func TestStatusRecorder_PushUnsupported(t *testing.T) {
	// 底层 ResponseWriter 不支持 Push 时返回错误
	w := httptest.NewRecorder()
	rec := newStatusRecorder(w)
	err := rec.Push("/asset.js", nil)
	assert.Error(t, err)
}

// --- timeoutWriter 单元测试 ---

func newTestTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{
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
	tw.timedOut = true
	_, err := tw.Write([]byte("x"))
	assert.ErrorIs(t, err, http.ErrHandlerTimeout)
	assert.Empty(t, tw.wbuf.Bytes())
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

// --- cryptionResponseWriter 单元测试 ---

func TestCryptionWriter_WriteBuffers(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &cryptionResponseWriter{ResponseWriter: w}

	n, err := cw.Write([]byte("secret data"))
	assert.NoError(t, err)
	assert.Equal(t, 11, n)
	// 写入缓冲而非直接写到底层（待加密）
	assert.Equal(t, "secret data", cw.buf.String())
	assert.Empty(t, w.Body.String())
}

func TestCryptionWriter_WriteHeaderRecords(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &cryptionResponseWriter{ResponseWriter: w}

	cw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, cw.code)
}

func TestCryptionWriter_HeaderDelegates(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &cryptionResponseWriter{ResponseWriter: w}

	cw.Header().Set("X-Test", "1")
	assert.Equal(t, "1", w.Header().Get("X-Test"))
}
