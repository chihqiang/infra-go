package respw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecorderWriter_DefaultStatus(t *testing.T) {
	// handler 不调 WriteHeader，直接 Write，status 应为 200
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	n, err := rec.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, rec.Status(), "未显式 WriteHeader 时默认 200")
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
	// 多次 WriteHeader 只记录第一次（符合 HTTP 规范）
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
	// Unwrap 应返回底层 ResponseWriter，供 http.ResponseController 使用
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	assert.Same(t, w, rec.Unwrap())
}

func TestRecorderWriter_Flush(t *testing.T) {
	// 底层不支持 Flush 时静默忽略，不 panic
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	rec.Flush()
	assert.True(t, w.Flushed)
}

func TestRecorderWriter_HijackUnsupported(t *testing.T) {
	// 底层 ResponseWriter 不支持 Hijack 时返回错误
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	_, _, err := rec.Hijack()
	assert.Error(t, err)
}

func TestRecorderWriter_PushUnsupported(t *testing.T) {
	// 底层 ResponseWriter 不支持 Push 时返回错误
	w := httptest.NewRecorder()
	rec := NewRecorderWriter(w)
	err := rec.Push("/asset.js", nil)
	assert.Error(t, err)
}
