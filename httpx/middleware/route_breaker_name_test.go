package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/stretchr/testify/assert"
)

// This file covers "breaker names must be bounded" (regression).
//
// Historical defect: RouteBreaker used `METHOD + r.URL.Path` (the concrete path, e.g.
// /users/1, /users/2, ...) as the breaker name, and breaker.GetBreaker caches every name
// forever:
//   - "isolation per route" degraded into "isolation per request", making the breaker
//     statistics meaningless;
//   - every distinct path parameter created a new breaker -> unbounded memory growth.
//
// Regression assertion: when many distinct path parameters are requested, the number of
// breaker names produced must converge instead of growing along with them.

// TestNormalizePathPattern covers the path normalization rules.
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

// TestNormalizePathPattern_LongPathTruncated verifies an overly long path is truncated so
// names cannot bloat.
func TestNormalizePathPattern_LongPathTruncated(t *testing.T) {
	path := "/"
	for i := 0; i < 100; i++ {
		path += "seg/"
	}
	got := normalizePathPattern(path)
	assert.LessOrEqual(t, len(got), maxPatternSegments*5,
		"normalized pattern must stay bounded, got len=%d", len(got))
}

// TestBreakerName_UsesRequestPattern verifies the route template filled in by net/http is preferred.
func TestBreakerName_UsesRequestPattern(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.Pattern = "GET /users/{id}"
	assert.Equal(t, "GET /users/{id}", breakerName(r))
}

// TestBreakerName_UsesContextPattern verifies the global middleware case: when r.Pattern is
// empty, the template that httpx resolved ahead of time into the context is used.
func TestBreakerName_UsesContextPattern(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	assert.Empty(t, r.Pattern, "precondition: r.Pattern is not set for global middleware")

	ctx := ContextWithPattern(r.Context(), "GET /users/{id}")
	r = r.WithContext(ctx)

	assert.Equal(t, "GET /users/{id}", breakerName(r))
}

// TestBreakerName_FallsBackToNormalizedPath verifies the fallback to the normalized path when
// there is no template.
func TestBreakerName_FallsBackToNormalizedPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	assert.Equal(t, "GET:/users/{}", breakerName(r))
}

// TestBreakerName_BoundedCardinality is a regression test: many distinct path parameters
// must converge to a finite set of breaker names.
func TestBreakerName_BoundedCardinality(t *testing.T) {
	names := make(map[string]struct{})

	// simulate 1000 distinct user IDs
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

// TestBreakerName_BoundedCardinalityWithPattern is the same, but goes through r.Pattern.
func TestBreakerName_BoundedCardinalityWithPattern(t *testing.T) {
	names := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		r := httptest.NewRequest(http.MethodGet, "/users/"+itoa(i), nil)
		r.Pattern = "GET /users/{id}"
		names[breakerName(r)] = struct{}{}
	}
	assert.Len(t, names, 1)
}

// TestRouteBreaker_RegistryBounded is an end-to-end regression: after going through the
// middleware, the breaker registry does not grow with the number of path parameters.
func TestRouteBreaker_RegistryBounded(t *testing.T) {
	mw := NewRouteBreaker().Middleware()
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	const requests = 300
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/items/"+itoa(i), nil)
		// simulate the httpx global middleware: the template is written into the context upstream
		req = req.WithContext(ContextWithPattern(req.Context(), "GET /items/{id}"))
		rec := perform(mw, next, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// this route should produce a single breaker (not 300 of them)
	before := breaker.RegistrySize()
	// running again with different parameters must not keep growing the registry
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/items/"+itoa(i+1000), nil)
		req = req.WithContext(ContextWithPattern(req.Context(), "GET /items/{id}"))
		perform(mw, next, req)
	}
	assert.Equal(t, before, breaker.RegistrySize(),
		"breaker registry must not grow with the number of distinct path parameters")
}

// TestRouteBreaker_DifferentRoutesGetDifferentBreakers verifies different routes stay isolated.
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

// itoa avoids pulling in strconv (a small test-local helper).
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
