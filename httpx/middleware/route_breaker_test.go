package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 route_breaker.go：按路由隔离的熔断中间件。

func TestRouteBreaker_Allows(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewRouteBreaker().Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouteBreaker_Isolation(t *testing.T) {
	silenceLogger(t)
	// 同一中间件下：/fail 熔断不影响 /ok（按 METHOD:path 隔离）
	mw := NewRouteBreaker().Middleware()
	fail := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	var rejected bool
	for i := 0; i < 2000; i++ {
		if rec := perform(mw, fail, httptest.NewRequest(http.MethodGet, "/fail", nil)); rec.Code == http.StatusServiceUnavailable {
			rejected = true
			break
		}
	}
	assert.True(t, rejected, "/fail 应最终被熔断")

	// 隔离生效：/ok 仍可正常通过
	rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
