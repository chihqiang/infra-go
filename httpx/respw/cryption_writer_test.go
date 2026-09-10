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
	// 写入缓冲而非直接写到底层（待加密）
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
	cw := NewCryptionWriter(w, 4) // 缓冲上限 4 字节

	_, err := cw.Write([]byte("abc"))
	assert.NoError(t, err)
	assert.False(t, cw.Overflowed())
	assert.Empty(t, w.Body.String(), "缓冲模式不应写到底层")

	// 超出上限：切换为明文透传，已缓冲内容 + 本次内容都应原样写到底层
	n, err := cw.Write([]byte("def"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.True(t, cw.Overflowed())
	assert.Equal(t, "abcdef", w.Body.String(), "已缓冲内容与后续内容都必须完整输出，不得截断")
	assert.Empty(t, cw.Buffered())

	// 进入透传模式后继续写入仍完整输出
	_, err = cw.Write([]byte("ghi"))
	assert.NoError(t, err)
	assert.Equal(t, "abcdefghi", w.Body.String())
}

func TestCryptionWriter_OverflowWritesStatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	cw := NewCryptionWriter(w, 2)

	cw.WriteHeader(http.StatusCreated)
	_, _ = cw.Write([]byte("ab"))
	_, _ = cw.Write([]byte("cd")) // 触发透传

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "abcd", w.Body.String())
}

func TestCryptionWriter_OverflowFlushes(t *testing.T) {
	// 透传模式下 Flush 应到达底层（保证大响应可流式输出）
	underlying := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	cw := NewCryptionWriter(underlying, 2)

	_, _ = cw.Write([]byte("ab"))
	_, _ = cw.Write([]byte("cd")) // 触发透传
	cw.Flush()

	assert.True(t, underlying.flushed)
	assert.Equal(t, "abcd", underlying.Body.String())
}

func TestCryptionWriter_FlushIsNoop(t *testing.T) {
	// 加密需整体缓冲后输出，Flush 不应向底层透传（避免"半发送"空响应）
	underlying := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	cw := &CryptionWriter{ResponseWriter: underlying}

	cw.WriteHeader(http.StatusOK)
	cw.Flush()

	assert.False(t, underlying.flushed, "Flush must not reach underlying writer")
	assert.Empty(t, underlying.Body.String())
}

// flushRecorder 记录是否收到 Flush 调用的测试 writer。
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() { f.flushed = true }
