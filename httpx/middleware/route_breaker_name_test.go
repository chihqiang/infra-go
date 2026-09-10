package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/stretchr/testify/assert"
)

// 本文件覆盖"熔断器名称必须有界"（回归）。
//
// 历史缺陷：RouteBreaker 用 `METHOD + r.URL.Path`（具体路径，如 /users/1、
// /users/2 …）作为熔断器名称，而 breaker.GetBreaker 会永久缓存每个名称：
//   - "按路由隔离"退化为"按请求隔离"，熔断统计失去意义；
//   - 每个不同的路径参数都新建一个熔断器 → 内存无界增长。
//
// 回归断言：请求大量不同的路径参数时，产生的熔断器名称数量必须收敛（而不是随之增长）。

// TestNormalizePathPattern 覆盖路径归一化规则。
func TestNormalizePathPattern(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"numeric id", "/users/123", "/users/{}"},
		{"multiple numeric", "/users/123/orders/456", "/users/{}/orders/{}"},
		{"uuid", "/files/12345678-1234-1234-1234-123456789abc", "/files/{}"},
		{"long hex", "/blobs/a1b2c3d4e5f60718", "/blobs/{}"},
		{"long opaque token", "/t/abcdefghijklmnopqrst", "/t/{}"},
		{"no id", "/users", "/users"},
		{"static segments kept", "/api/v1/users", "/api/v1/users"},
		{"single digit id", "/users/0", "/users/{}"},
		{"short letter segment kept", "/v1/a", "/v1/a"},
		{"trailing slash preserved", "/users/123/", "/users/{}/"},
		{"root", "/", "/"},
		{"empty", "", "/"},
		{"words not treated as ids", "/users/current/profile", "/users/current/profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizePathPattern(tt.in))
		})
	}
}

// TestNormalizePathPattern_LongPathTruncated 验证超长路径被截断，避免名称膨胀。
func TestNormalizePathPattern_LongPathTruncated(t *testing.T) {
	path := "/"
	for i := 0; i < 100; i++ {
		path += "seg/"
	}
	got := normalizePathPattern(path)
	assert.LessOrEqual(t, len(got), maxPatternSegments*5,
		"normalized pattern must stay bounded, got len=%d", len(got))
}

// TestBreakerName_UsesRequestPattern 验证优先使用 net/http 填充的路由模板。
func TestBreakerName_UsesRequestPattern(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.Pattern = "GET /users/{id}"
	assert.Equal(t, "GET /users/{id}", breakerName(r))
}

// TestBreakerName_UsesContextPattern 验证全局中间件场景：
// r.Pattern 为空时使用 httpx 预判并写入 context 的模板。
func TestBreakerName_UsesContextPattern(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	assert.Empty(t, r.Pattern, "precondition: r.Pattern is not set for global middleware")

	ctx := ContextWithPattern(r.Context(), "GET /users/{id}")
	r = r.WithContext(ctx)

	assert.Equal(t, "GET /users/{id}", breakerName(r))
}

// TestBreakerName_FallsBackToNormalizedPath 验证无模板时回退到归一化路径。
func TestBreakerName_FallsBackToNormalizedPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	assert.Equal(t, "GET:/users/{}", breakerName(r))
}

// TestBreakerName_BoundedCardinality 回归测试：大量不同的路径参数
// 必须收敛到有限的熔断器名称集合。
func TestBreakerName_BoundedCardinality(t *testing.T) {
	names := make(map[string]struct{})

	// 模拟 1000 个不同的用户 ID
	for i := 0; i < 1000; i++ {
		r := httptest.NewRequest(http.MethodGet, "/users/"+itoa(i), nil)
		names[breakerName(r)] = struct{}{}
		if len(names) > 5 {
			t.Fatalf("breaker names are not bounded: %d distinct names after %d requests", len(names), i+1)
		}
	}

	assert.Len(t, names, 1, "all ids of one route must map to a single breaker name")
	_, ok := names["GET:/users/{}"]
	assert.True(t, ok, "expected normalized name, got %v", names)
}

// TestBreakerName_BoundedCardinalityWithPattern 同上，但走 r.Pattern 路径。
func TestBreakerName_BoundedCardinalityWithPattern(t *testing.T) {
	names := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		r := httptest.NewRequest(http.MethodGet, "/users/"+itoa(i), nil)
		r.Pattern = "GET /users/{id}"
		names[breakerName(r)] = struct{}{}
	}
	assert.Len(t, names, 1)
}

// TestRouteBreaker_RegistryBounded 端到端回归：经过中间件处理后，
// breaker 注册表不会随路径参数数量增长。
func TestRouteBreaker_RegistryBounded(t *testing.T) {
	mw := NewRouteBreaker().Middleware()
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	const requests = 300
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/items/"+itoa(i), nil)
		// 模拟 httpx 全局中间件：模板由上层写入 context
		req = req.WithContext(ContextWithPattern(req.Context(), "GET /items/{id}"))
		rec := perform(mw, next, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// 该路由只应产生一个熔断器（而不是 300 个）
	before := breaker.RegistrySize()
	// 再跑一遍不同的参数，注册表不应继续增长
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/items/"+itoa(i+1000), nil)
		req = req.WithContext(ContextWithPattern(req.Context(), "GET /items/{id}"))
		perform(mw, next, req)
	}
	assert.Equal(t, before, breaker.RegistrySize(),
		"breaker registry must not grow with the number of distinct path parameters")
}

// TestRouteBreaker_DifferentRoutesGetDifferentBreakers 验证不同路由仍然隔离。
func TestRouteBreaker_DifferentRoutesGetDifferentBreakers(t *testing.T) {
	mw := NewRouteBreaker().Middleware()
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	before := breaker.RegistrySize()

	for _, pattern := range []string{"GET /a/{id}", "GET /b/{id}", "POST /a/{id}"} {
		req := httptest.NewRequest(http.MethodGet, "/x/1", nil)
		req = req.WithContext(ContextWithPattern(req.Context(), pattern))
		perform(mw, next, req)
	}

	assert.Equal(t, before+3, breaker.RegistrySize(),
		"each distinct route template must get its own breaker")
}

// itoa 避免引入 strconv（测试内的小工具）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
