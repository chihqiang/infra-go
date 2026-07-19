package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- JSON 绑定测试 ---

type userRequest struct {
	Name  string `json:"name" binding:"required"`
	Age   int    `json:"age" binding:"gte=0,lte=150"`
	Email string `json:"email" binding:"required,email"`
}

func TestBindJSON(t *testing.T) {
	body := `{"name":"Alice","age":25,"email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", MIMEJSON)

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
	req.Header.Set("Content-Type", MIMEJSON)

	var user userRequest
	err := BindJSON(req, &user)
	assert.Error(t, err)
}

func TestBindJSON_InvalidJSON(t *testing.T) {
	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", MIMEJSON)

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
	req.Header.Set("Content-Type", MIMEPOSTForm)

	var f formRequest
	err := BindForm(req, &f)
	require.NoError(t, err)
	assert.Equal(t, "admin", f.Username)
	assert.Equal(t, "123456", f.Password)
	assert.True(t, f.Remember)
}

func TestBindForm_ValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("username=admin&password=123"))
	req.Header.Set("Content-Type", MIMEPOSTForm)

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
	req.Header.Set("Content-Type", MIMEJSON)

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
	req.Header.Set("Content-Type", MIMEPOSTForm)

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
	req.Header.Set("Content-Type", MIMEJSON)
	w := httptest.NewRecorder()

	var user userRequest
	err := MustBindJSON(w, req, &user)
	require.NoError(t, err)
	assert.Equal(t, "Alice", user.Name)
}

func TestMustBindJSON_Error(t *testing.T) {
	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", MIMEJSON)
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

func TestMustBindURI_Success(t *testing.T) {
	params := map[string]string{"id": "123", "category": "books"}
	w := httptest.NewRecorder()

	var u uriRequest
	err := MustBindURI(w, params, &u)
	require.NoError(t, err)
	assert.Equal(t, 123, u.ID)
}

func TestMustBindURI_Error(t *testing.T) {
	params := map[string]string{"category": "books"}
	w := httptest.NewRecorder()

	var u uriRequest
	err := MustBindURI(w, params, &u)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- JSON 响应测试 ---

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"hello": "world"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hello")
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestOkJSON_Success(t *testing.T) {
	w := httptest.NewRecorder()
	OkJSON(w, map[string]string{"name": "Alice"})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[map[string]string]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeOK, resp.Code)
	assert.Equal(t, MsgOK, resp.Msg)
	assert.Equal(t, "Alice", resp.Data["name"])
}

func TestOkJSON_Struct(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	w := httptest.NewRecorder()
	OkJSON(w, User{Name: "Bob", Age: 30})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[User]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeOK, resp.Code)
	assert.Equal(t, MsgOK, resp.Msg)
	assert.Equal(t, "Bob", resp.Data.Name)
	assert.Equal(t, 30, resp.Data.Age)
}

func TestOkJSON_CodeError(t *testing.T) {
	w := httptest.NewRecorder()
	OkJSON(w, NewCodeError(CodeBadRequest, "invalid parameter"))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[any]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeBadRequest, resp.Code)
	assert.Equal(t, "invalid parameter", resp.Msg)
}

func TestOkJSON_CodeErrorPointer(t *testing.T) {
	w := httptest.NewRecorder()
	codeErr := NewCodeError(CodeUnauthorized, "token expired")
	OkJSON(w, codeErr)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[any]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeUnauthorized, resp.Code)
	assert.Equal(t, "token expired", resp.Msg)
}

func TestOkJSON_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	OkJSON(w, errors.New("something went wrong"))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[any]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeDefaultError, resp.Code)
	assert.Equal(t, "something went wrong", resp.Msg)
}

func TestOkJSONCtx(t *testing.T) {
	w := httptest.NewRecorder()
	OkJSONCtx(context.TODO(), w, map[string]string{"key": "value"})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[map[string]string]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, CodeOK, resp.Code)
	assert.Equal(t, "value", resp.Data["key"])
}

func TestWriteHTTPError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteHTTPError(w, http.StatusNotFound, "not found")
	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp Response[any]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.Code)
	assert.Equal(t, "not found", resp.Msg)
}

// --- XML 响应测试 ---

type xmlMessage struct {
	XMLName xml.Name `xml:"data"`
	Name    string   `xml:"name"`
}

func TestWriteXML(t *testing.T) {
	w := httptest.NewRecorder()
	msg := xmlMessage{Name: "anyone"}
	WriteXML(w, http.StatusOK, msg)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "<data><name>anyone</name></data>", w.Body.String())
}

func TestOkXML_Success(t *testing.T) {
	w := httptest.NewRecorder()
	OkXML(w, xmlMessage{Name: "anyone"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<code>0</code>`)
	assert.Contains(t, w.Body.String(), `<msg>ok</msg>`)
	assert.Contains(t, w.Body.String(), `<name>anyone</name>`)
}

func TestOkXML_CodeError(t *testing.T) {
	w := httptest.NewRecorder()
	OkXML(w, NewCodeError(1, "test"))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<code>1</code>`)
	assert.Contains(t, w.Body.String(), `<msg>test</msg>`)
}

func TestOkXML_Error(t *testing.T) {
	w := httptest.NewRecorder()
	OkXML(w, errors.New("test"))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<code>-1</code>`)
	assert.Contains(t, w.Body.String(), `<msg>test</msg>`)
}

func TestOkXMLCtx(t *testing.T) {
	w := httptest.NewRecorder()
	OkXMLCtx(context.TODO(), w, xmlMessage{Name: "anyone"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<name>anyone</name>`)
}

func TestWriteXML_MarshalError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteXML(w, http.StatusOK, map[string]any{
		"Data": complex(0, 0),
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- HTML 响应测试 ---

func TestWriteHTML(t *testing.T) {
	w := httptest.NewRecorder()
	html := "<h1>Hello, World!</h1>"
	WriteHTML(w, http.StatusOK, html)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, html, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestOkHTML(t *testing.T) {
	w := httptest.NewRecorder()
	html := "<h1>Hello, World!</h1>"
	OkHTML(w, html)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, html, w.Body.String())
}

func TestOkHTMLCtx(t *testing.T) {
	w := httptest.NewRecorder()
	html := "<h1>Hello, World!</h1>"
	OkHTMLCtx(context.TODO(), w, html)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, html, w.Body.String())
}

// --- CodeError 测试 ---

func TestCodeError_Error(t *testing.T) {
	err := NewCodeError(CodeBadRequest, "bad request")
	assert.Equal(t, "bad request", err.Error())
}

func TestCodeError_WithCause(t *testing.T) {
	cause := errors.New("database connection failed")
	err := NewCodeErrorWithCause(CodeInternalError, "internal error", cause)
	assert.Contains(t, err.Error(), "internal error")
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestCodeError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := NewCodeErrorWithCause(CodeInternalError, "wrapped", cause)
	assert.True(t, errors.Is(err, cause))
}

// --- ParseJSON 测试 ---

func TestParseJSON(t *testing.T) {
	body := `{"name":"Alice","age":25}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var result struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	err := ParseJSON(req, &result)
	require.NoError(t, err)
	assert.Equal(t, "Alice", result.Name)
	assert.Equal(t, 25, result.Age)
}

func TestParseJSON_WithValidation(t *testing.T) {
	body := `{"name":"","age":25}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var result struct {
		Name string `json:"name" binding:"required"`
		Age  int    `json:"age"`
	}
	err := ParseJSON(req, &result)
	assert.Error(t, err)
}

func TestParseJSONWithLimit(t *testing.T) {
	body := `{"name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var result struct {
		Name string `json:"name"`
	}
	err := ParseJSONWithLimit(req, &result, 1024)
	require.NoError(t, err)
	assert.Equal(t, "Alice", result.Name)
}

func TestParseJSONWithLimit_Exceeded(t *testing.T) {
	body := `{"name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var result struct {
		Name string `json:"name"`
	}
	err := ParseJSONWithLimit(req, &result, 5)
	assert.Error(t, err)
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
	req.Header.Set("Content-Type", MIMEJSON)

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
	req.Header.Set("Content-Type", MIMEJSON)

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
	req.Header.Set("Content-Type", MIMEJSON)

	var r pointerRequest
	err := BindJSON(req, &r)
	require.NoError(t, err)
	assert.Nil(t, r.Name)
	assert.Nil(t, r.Age)
	require.NotNil(t, r.Email)
}

// --- BindBody 测试 ---

func TestJSON_BindBody(t *testing.T) {
	body := []byte(`{"name":"Alice","age":25,"email":"alice@example.com"}`)

	var user userRequest
	err := JSON.(BindingBody).BindBody(body, &user)
	require.NoError(t, err)
	assert.Equal(t, "Alice", user.Name)
}

// --- Default 绑定器选择测试 ---

func TestDefault_GET(t *testing.T) {
	b := Default(http.MethodGet, "")
	assert.Equal(t, "form", b.Name())
}

func TestDefault_PostJSON(t *testing.T) {
	b := Default(http.MethodPost, MIMEJSON)
	assert.Equal(t, "json", b.Name())
}

func TestDefault_PostForm(t *testing.T) {
	b := Default(http.MethodPost, MIMEPOSTForm)
	assert.Equal(t, "form", b.Name())
}

func TestDefault_PostMultipart(t *testing.T) {
	b := Default(http.MethodPost, MIMEMultipartPOSTForm)
	assert.Equal(t, "form", b.Name())
}

// --- 响应结构序列化测试 ---

func TestResponse_Serialization(t *testing.T) {
	resp := Response[string]{
		Code: CodeOK,
		Msg:  MsgOK,
		Data: "hello",
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"code":0`)
	assert.Contains(t, string(data), `"msg":"ok"`)
	assert.Contains(t, string(data), `"data":"hello"`)
}

func TestResponse_EmptyData(t *testing.T) {
	resp := Response[string]{
		Code: CodeOK,
		Msg:  MsgOK,
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	// data 为零值时 omitempty 应跳过
	assert.NotContains(t, string(data), `"data"`)
}

func TestResponse_WithSlice(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	resp := Response[[]Item]{
		Code: CodeOK,
		Msg:  MsgOK,
		Data: []Item{
			{ID: 1, Name: "a"},
			{ID: 2, Name: "b"},
		},
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var result Response[[]Item]
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, "a", result.Data[0].Name)
}
