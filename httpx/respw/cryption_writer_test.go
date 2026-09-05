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
