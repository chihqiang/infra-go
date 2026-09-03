package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- URL Query 参数 ---

func TestQueryValue_String(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users?tag=a&empty=&num=42", nil)

	// 泛型类型推断：无 def 时需显式指定类型实参
	assert.Equal(t, "a", QueryValue[string](r, "tag"))
	assert.Equal(t, "", QueryValue[string](r, "empty"))
	assert.Equal(t, "", QueryValue[string](r, "missing"))

	// 带默认值：def 类型即 T
	assert.Equal(t, "a", QueryValue(r, "tag", "fb"))
	assert.Equal(t, "fb", QueryValue(r, "empty", "fb"))
	assert.Equal(t, "fb", QueryValue(r, "missing", "fb"))
}

func TestQueryValue_Typed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/users?page=2&limit=9223372036854775807&ratio=3.14&flag=true", nil)

	assert.Equal(t, 2, QueryValue(r, "page", -1)) // T 推断为 int
	assert.Equal(t, int64(9223372036854775807), QueryValue[int64](r, "limit", -1))
	assert.Equal(t, uint64(2), QueryValue[uint64](r, "page", 0))
	assert.Equal(t, 3.14, QueryValue(r, "ratio", -1.0))
	assert.Equal(t, true, QueryValue(r, "flag", false))
}

func TestQueryValue_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users?page=abc&flag=zzz", nil)

	// 缺失/非法 → 默认值
	assert.Equal(t, 7, QueryValue(r, "missing", 7))
	assert.Equal(t, 7, QueryValue(r, "page", 7))
	assert.Equal(t, int64(7), QueryValue[int64](r, "page", 7))
	assert.Equal(t, uint64(7), QueryValue[uint64](r, "page", 7))
	assert.Equal(t, 7.5, QueryValue(r, "ratio", 7.5))
	assert.Equal(t, true, QueryValue(r, "flag", true))

	// 未提供默认值 → 类型零值
	assert.Equal(t, 0, QueryValue[int](r, "missing"))
	assert.Equal(t, "", QueryValue[string](r, "missing"))
}

// --- 路径参数 ---

func TestPathValue_Typed(t *testing.T) {
	mux := http.NewServeMux()
	var (
		id    int
		name  string
		ratio float64
	)
	mux.HandleFunc("GET /users/{id}/{name}", func(w http.ResponseWriter, r *http.Request) {
		id = PathValue(r, "id", -1)
		name = PathValue[string](r, "name")
		ratio = PathValue(r, "id", -1.0)
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42/bob", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 42, id)
	assert.Equal(t, "bob", name)
	assert.Equal(t, 42.0, ratio)
}

func TestPathValue_Defaults(t *testing.T) {
	mux := http.NewServeMux()
	var (
		missing int
		id      string
		count   uint64
	)
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		missing = PathValue(r, "none", 5) // 无此路径参数 → 默认
		id = PathValue(r, "id", "fb")     // 命中 id，返回实际值
		count = PathValue[uint64](r, "id", 0)
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 5, missing)
	assert.Equal(t, "42", id)
	assert.Equal(t, uint64(42), count)
}

func TestPathValue_Invalid(t *testing.T) {
	mux := http.NewServeMux()
	var id int
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id = PathValue(r, "id", 9)
	})
	req := httptest.NewRequest(http.MethodGet, "/users/abc", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 9, id)
}

// --- Header 请求头 ---

func TestHeaderValue_Values(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Token", "t-1")
	r.Header.Set("X-Count", "3")
	r.Header.Set("X-Flag", "true")

	assert.Equal(t, "t-1", HeaderValue(r, "X-Token", ""))
	assert.Equal(t, "t-1", HeaderValue(r, "x-token", "")) // 不区分大小写
	assert.Equal(t, "fb", HeaderValue(r, "X-Missing", "fb"))
	assert.Equal(t, 3, HeaderValue(r, "X-Count", -1))
	assert.Equal(t, int64(3), HeaderValue[int64](r, "X-Count", -1))
	assert.Equal(t, true, HeaderValue(r, "X-Flag", false))

	// 缺失 → 默认
	assert.Equal(t, -1, HeaderValue(r, "X-Missing", -1))
}

func TestHeaderValue_Invalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Count", "abc")
	assert.Equal(t, 9, HeaderValue(r, "X-Count", 9))
}

// --- nil 请求安全性 ---

func TestValue_NilRequest(t *testing.T) {
	assert.Equal(t, "", QueryValue[string](nil, "k"))
	assert.Equal(t, 0, QueryValue[int](nil, "k"))
	assert.Equal(t, 9, QueryValue(nil, "k", 9))
	assert.Equal(t, "fb", HeaderValue(nil, "k", "fb"))
}
