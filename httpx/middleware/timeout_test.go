package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 对应 timeout.go：请求超时中间件。

func TestTimeout_DisabledWhenNonPositive(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewTimeout(0).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTimeout_CompletesWithinDeadline(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // 小于超时
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewTimeout(time.Second).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTimeout_TimesOutSlowHandler(t *testing.T) {
	slow := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}

	rec := perform(NewTimeout(30*time.Millisecond).Middleware(), slow,
		httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTimeout_WebSocketExempt(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// WebSocket 升级请求豁免超时：即使耗时超限也直接透传（此用例验证不触发超时写入）
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := perform(NewTimeout(1*time.Millisecond).Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
