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
	// 超出上限：标记 overflowed 并返回错误
	_, err = cw.Write([]byte("def"))
	assert.Error(t, err)
	assert.True(t, cw.Overflowed())
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
