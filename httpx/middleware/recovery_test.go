package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 recovery.go：Recovery 中间件。

func TestRecovery_Panic(t *testing.T) {
	silenceLogger(t)
	panicHandler := func(w http.ResponseWriter, r *http.Request) { panic("boom") }

	rec := perform(NewRecovery().Middleware(), panicHandler,
		httptest.NewRequest(http.MethodGet, "/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
}

func TestRecovery_Normal(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewRecovery().Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRecovery_StillRunsAfterPanic(t *testing.T) {
	silenceLogger(t)
	// panic 恢复后，同一中间件实例仍可正常服务后续请求
	mw := NewRecovery().Middleware()

	rec1 := perform(mw, func(w http.ResponseWriter, r *http.Request) { panic("boom") },
		httptest.NewRequest(http.MethodGet, "/panic", nil))
	assert.Equal(t, http.StatusInternalServerError, rec1.Code)

	rec2 := perform(mw, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec2.Code)
}

func TestRecovery_ComposeWithRequestID(t *testing.T) {
	// 标准 net/http 组合：RequestID → Recovery → 业务（验证不依赖 httpx，可被其它框架复用）
	silenceLogger(t)
	var capturedID string
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		panic("boom")
	})

	handler := NewRecovery().Middleware()(NewRequestID().Middleware()(final))
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(HeaderRequestID))
	assert.Equal(t, rec.Header().Get(HeaderRequestID), capturedID)
}
