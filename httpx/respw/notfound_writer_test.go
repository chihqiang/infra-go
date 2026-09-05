package respw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotFoundResponseWriter_Intercepts404(t *testing.T) {
	rec := httptest.NewRecorder()

	// 自定义 404 处理器
	var called int
	handler := func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"msg":"custom"}`))
	}

	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := NewNotFoundResponseWriter(rec, r, handler)

	// 模拟 ServeMux 写入 404
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("404 page not found\n"))

	assert.Equal(t, 1, called)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, `{"code":404,"msg":"custom"}`, rec.Body.String())
}

func TestNotFoundResponseWriter_PassthroughNon404(t *testing.T) {
	rec := httptest.NewRecorder()
	handler := func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for non-404 status")
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := NewNotFoundResponseWriter(rec, r, handler)

	w.WriteHeader(http.StatusOK)
	n, err := w.Write([]byte("ok"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestNotFoundResponseWriter_HandlerOnlyOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	var called int
	handler := func(w http.ResponseWriter, r *http.Request) {
		called++
		_, _ = w.Write([]byte("custom"))
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := NewNotFoundResponseWriter(rec, r, handler)

	// 多次 WriteHeader(404)，handler 只应触发一次
	w.WriteHeader(http.StatusNotFound)
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("body"))
	_, _ = w.Write([]byte("again"))

	assert.Equal(t, 1, called)
	assert.Equal(t, "custom", rec.Body.String())
}

func TestNotFoundResponseWriter_WriteBeforeHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("custom"))
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := NewNotFoundResponseWriter(rec, r, handler)

	// 直接 Write：未 suppress，透传给底层
	_, _ = w.Write([]byte("hello"))
	assert.Equal(t, "hello", rec.Body.String())
}
