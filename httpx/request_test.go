package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/httpx/binding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- JSON 绑定测试 ---

type userRequest struct {
	Name  string `json:"name" binding:"required"`
	Age   int    `json:"age" binding:"gte=0,lte=150"`
	Email string `json:"email" binding:"required,email"`
}

func TestBindJSON(t *testing.T) {
	body := `{"name":"Alice","age":25,"email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var user userRequest
	err := BindJSON(req, &user)
	require.NoError(t, err)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, 25, user.Age)
	assert.Equal(t, "alice@example.com", user.Email)
}

func TestBindJSON_ValidationError(t *testing.T) {
	body := `{"name":"","age":200,"email":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var user userRequest
	err := BindJSON(req, &user)
	assert.Error(t, err)
}

func TestBindJSON_InvalidJSON(t *testing.T) {
	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var user userRequest
	err := BindJSON(req, &user)
	assert.Error(t, err)
}

func TestBindJSON_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	var user userRequest
	err := BindJSON(req, &user)
	assert.Error(t, err)
}

// --- Query 绑定测试 ---

type queryRequest struct {
	Page     int    `form:"page" binding:"required,gte=1"`
	PageSize int    `form:"page_size" binding:"required,gte=1,lte=100"`
	Keyword  string `form:"keyword"`
	Sort     string `form:"sort,default=desc"`
}

func TestBindQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=1&page_size=20&keyword=hello", nil)

	var q queryRequest
	err := BindQuery(req, &q)
	require.NoError(t, err)
	assert.Equal(t, 1, q.Page)
	assert.Equal(t, 20, q.PageSize)
	assert.Equal(t, "hello", q.Keyword)
	assert.Equal(t, "desc", q.Sort) // 默认值
}

func TestBindQuery_DefaultValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=1&page_size=10", nil)

	var q queryRequest
	err := BindQuery(req, &q)
	require.NoError(t, err)
	assert.Equal(t, "desc", q.Sort)
}

func TestBindQuery_ValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=0&page_size=200", nil)

	var q queryRequest
	err := BindQuery(req, &q)
	assert.Error(t, err)
}

func TestBindQuery_MissingRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?keyword=test", nil)

	var q queryRequest
	err := BindQuery(req, &q)
	assert.Error(t, err)
}

// --- Form 绑定测试 ---

type formRequest struct {
	Username string `form:"username" binding:"required"`
	Password string `form:"password" binding:"required,min=6"`
	Remember bool   `form:"remember"`
}

func TestBindForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("username=admin&password=123456&remember=true"))
	req.Header.Set("Content-Type", binding.MIMEPOSTForm)

	var f formRequest
	err := BindForm(req, &f)
	require.NoError(t, err)
	assert.Equal(t, "admin", f.Username)
	assert.Equal(t, "123456", f.Password)
	assert.True(t, f.Remember)
}

func TestBindForm_ValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("username=admin&password=123"))
	req.Header.Set("Content-Type", binding.MIMEPOSTForm)

	var f formRequest
	err := BindForm(req, &f)
	assert.Error(t, err)
}

// --- Header 绑定测试 ---

type headerRequest struct {
	AuthToken string `header:"X-Auth-Token" binding:"required"`
	TraceID   string `header:"X-Trace-Id"`
	Version   string `header:"X-Version,default=v1"`
}

func TestBindHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Auth-Token", "token123")
	req.Header.Set("X-Trace-Id", "trace456")

	var h headerRequest
	err := BindHeader(req, &h)
	require.NoError(t, err)
	assert.Equal(t, "token123", h.AuthToken)
	assert.Equal(t, "trace456", h.TraceID)
	assert.Equal(t, "v1", h.Version) // 默认值
}

func TestBindHeader_MissingRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	var h headerRequest
	err := BindHeader(req, &h)
	assert.Error(t, err)
}

// --- URI 绑定测试 ---

type uriRequest struct {
	ID       int    `uri:"id" binding:"required"`
	Category string `uri:"category"`
}

func TestBindURI(t *testing.T) {
	params := map[string]string{
		"id":       "123",
		"category": "books",
	}

	var u uriRequest
	err := BindURI(params, &u)
	require.NoError(t, err)
	assert.Equal(t, 123, u.ID)
	assert.Equal(t, "books", u.Category)
}

func TestBindURI_ValidationError(t *testing.T) {
	params := map[string]string{
		"category": "books",
	}

	var u uriRequest
	err := BindURI(params, &u)
	assert.Error(t, err)
}

// --- 自动绑定（Bind）测试 ---

func TestBind_AutoDetect_Get(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?name=Bob&age=30", nil)

	var result struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}
	err := Bind(req, &result)
	require.NoError(t, err)
	assert.Equal(t, "Bob", result.Name)
	assert.Equal(t, 30, result.Age)
}

func TestBind_AutoDetect_PostJSON(t *testing.T) {
	body := `{"name":"Charlie","age":35}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var result struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	err := Bind(req, &result)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", result.Name)
	assert.Equal(t, 35, result.Age)
}

func TestBind_AutoDetect_PostForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=David&age=40"))
	req.Header.Set("Content-Type", binding.MIMEPOSTForm)

	var result struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}
	err := Bind(req, &result)
	require.NoError(t, err)
	assert.Equal(t, "David", result.Name)
	assert.Equal(t, 40, result.Age)
}

// --- 各种类型绑定测试 ---

type typesRequest struct {
	Name      string        `form:"name"`
	Age       int           `form:"age"`
	Score     float64       `form:"score"`
	Active    bool          `form:"active"`
	Count     int64         `form:"count"`
	Tags      []string      `form:"tags"`
	CreatedAt time.Time     `form:"created_at" time_format:"2006-01-02"`
	Duration  time.Duration `form:"duration"`
	UName     string        `form:"uname"`
}

func TestBindQuery_Types(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?name=test&age=20&score=95.5&active=true&count=100&tags=a,b,c&created_at=2024-01-15&duration=5s&uname=hello", nil)

	var r typesRequest
	err := BindQuery(req, &r)
	require.NoError(t, err)
	assert.Equal(t, "test", r.Name)
	assert.Equal(t, 20, r.Age)
	assert.Equal(t, 95.5, r.Score)
	assert.True(t, r.Active)
	assert.Equal(t, int64(100), r.Count)
	assert.Equal(t, []string{"a", "b", "c"}, r.Tags)
	assert.Equal(t, 2024, r.CreatedAt.Year())
	assert.Equal(t, 5*time.Second, r.Duration)
	assert.Equal(t, "hello", r.UName)
}

func TestBindQuery_EmptyValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?name=&age=&score=&active=", nil)

	var r typesRequest
	err := BindQuery(req, &r)
	require.NoError(t, err)
	assert.Equal(t, "", r.Name)
	assert.Equal(t, 0, r.Age)
	assert.Equal(t, 0.0, r.Score)
	assert.False(t, r.Active)
}

// --- MustBind 系列测试 ---

func TestMustBindJSON_Success(t *testing.T) {
	body := `{"name":"Alice","age":25,"email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)
	w := httptest.NewRecorder()

	var user userRequest
	err := MustBindJSON(w, req, &user)
	require.NoError(t, err)
	assert.Equal(t, "Alice", user.Name)
}

func TestMustBindJSON_Error(t *testing.T) {
	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)
	w := httptest.NewRecorder()

	var user userRequest
	err := MustBindJSON(w, req, &user)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMustBindQuery_Success(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=1&page_size=20", nil)
	w := httptest.NewRecorder()

	var q queryRequest
	err := MustBindQuery(w, req, &q)
	require.NoError(t, err)
	assert.Equal(t, 1, q.Page)
}

func TestMustBindQuery_Error(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=0", nil)
	w := httptest.NewRecorder()

	var q queryRequest
	err := MustBindQuery(w, req, &q)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- 嵌套结构体测试 ---

type nestedRequest struct {
	User struct {
		Name    string `json:"name" binding:"required"`
		Address string `json:"address"`
	} `json:"user" binding:"required"`
	Metadata map[string]string `json:"metadata"`
}

func TestBindJSON_Nested(t *testing.T) {
	body := `{"user":{"name":"Alice","address":"123 Main St"},"metadata":{"key":"value"}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var r nestedRequest
	err := BindJSON(req, &r)
	require.NoError(t, err)
	assert.Equal(t, "Alice", r.User.Name)
	assert.Equal(t, "123 Main St", r.User.Address)
	assert.Equal(t, "value", r.Metadata["key"])
}

// --- 指针字段测试 ---

type pointerRequest struct {
	Name  *string `json:"name"`
	Age   *int    `json:"age"`
	Email *string `json:"email" binding:"required,email"`
}

func TestBindJSON_PointerFields(t *testing.T) {
	body := `{"name":"Alice","age":25,"email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var r pointerRequest
	err := BindJSON(req, &r)
	require.NoError(t, err)
	require.NotNil(t, r.Name)
	assert.Equal(t, "Alice", *r.Name)
	require.NotNil(t, r.Age)
	assert.Equal(t, 25, *r.Age)
	require.NotNil(t, r.Email)
	assert.Equal(t, "alice@example.com", *r.Email)
}

func TestBindJSON_PointerFields_Nil(t *testing.T) {
	body := `{"email":"bob@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", binding.MIMEJSON)

	var r pointerRequest
	err := BindJSON(req, &r)
	require.NoError(t, err)
	assert.Nil(t, r.Name)
	assert.Nil(t, r.Age)
	require.NotNil(t, r.Email)
}
