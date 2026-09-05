package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 对应 max_conns.go：并发连接数限制中间件。

func TestMaxConns_DisabledWhenNonPositive(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewMaxConns(0).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMaxConns_RejectsOverLimit(t *testing.T) {
	silenceLogger(t)
	mw := NewMaxConns(1).Middleware()

	release := make(chan struct{})
	slow := func(w http.ResponseWriter, r *http.Request) {
		<-release // 占用唯一并发名额直到测试放行
		w.WriteHeader(http.StatusOK)
	}

	// 第一个请求占用名额（在 goroutine 中阻塞）
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		mw(http.HandlerFunc(slow)).ServeHTTP(rec1, req1)
	}()

	// 等待名额被占用（给 goroutine 调度时间）
	time.Sleep(20 * time.Millisecond)

	// 第二个请求超限 → 503
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	rec2 := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec2.Code)

	// 释放名额，第一个请求完成
	close(release)
	<-done
	assert.Equal(t, http.StatusOK, rec1.Code)
}

func TestMaxConns_AllowsWithinLimit(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	mw := NewMaxConns(5).Middleware()
	for i := 0; i < 5; i++ {
		rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}
