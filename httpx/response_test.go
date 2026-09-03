package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	OkJSONCtx(context.Background(), w, map[string]string{"key": "value"})
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
	OkXMLCtx(context.Background(), w, xmlMessage{Name: "anyone"})
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
	OkHTMLCtx(context.Background(), w, html)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, html, w.Body.String())
}

// --- SSE 响应测试 ---

func TestSSEWriter_Headers(t *testing.T) {
	w := httptest.NewRecorder()
	NewSSEWriter(w)

	assert.Equal(t, ContentTypeSSE, w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))
}

func TestSSEWriter_Event(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.Event("message", "hello world")
	require.NoError(t, err)
	assert.True(t, w.Flushed, "write 后应自动 Flush")
	assert.Equal(t, "event: message\ndata: hello world\n\n", w.Body.String())
}

func TestSSEWriter_Data(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.Data("plain text")
	require.NoError(t, err)
	assert.Equal(t, "data: plain text\n\n", w.Body.String())
}

func TestSSEWriter_EventMultiLine(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.Event("log", "line1\nline2")
	require.NoError(t, err)
	assert.Equal(t, "event: log\ndata: line1\ndata: line2\n\n", w.Body.String())
}

func TestSSEWriter_JSONEvent(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.JSONEvent("order", map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, "event: order\ndata: {\"id\":\"1\"}\n\n", w.Body.String())
}

func TestSSEWriter_Comment(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.Comment("keep-alive")
	require.NoError(t, err)
	assert.Equal(t, ": keep-alive\n\n", w.Body.String())
}

func TestSSEWriter_Retry(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	err := sse.Retry(3000)
	require.NoError(t, err)
	assert.Equal(t, "retry: 3000\n\n", w.Body.String())
}

func TestSSEWriter_MultipleEvents(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	require.NoError(t, sse.Event("a", "1"))
	require.NoError(t, sse.Event("b", "2"))
	assert.Equal(t, "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n", w.Body.String())
}

// --- request_id 集成测试 ---

func TestOkJSONCtx_WithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	w := httptest.NewRecorder()
	OkJSONCtx(ctx, w, map[string]string{"name": "Alice"})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "req-123", resp.RequestID)
}

func TestOkJSONCtx_WithoutRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	OkJSONCtx(context.Background(), w, "hello")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "", resp.RequestID)
	assert.NotContains(t, w.Body.String(), "request_id")
}

func TestOkXMLCtx_WithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	w := httptest.NewRecorder()
	OkXMLCtx(ctx, w, xmlMessage{Name: "anyone"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<request_id>req-123</request_id>`)
}

func TestWriteHTTPErrorCtx_WithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	w := httptest.NewRecorder()
	WriteHTTPErrorCtx(ctx, w, http.StatusNotFound, "not found")
	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp Response[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, http.StatusNotFound, resp.Code)
	assert.Equal(t, "req-123", resp.RequestID)
}

func TestResponse_RequestIDOmitted(t *testing.T) {
	resp := Response[any]{Code: CodeOK, Msg: MsgOK}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "request_id")
}

func TestResponse_RequestIDSerialized(t *testing.T) {
	resp := Response[any]{Code: CodeOK, Msg: MsgOK, RequestID: "req-123"}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"request_id":"req-123"`)
}

// --- 重定向测试 ---

func newRedirectRequest(t *testing.T) (*httptest.ResponseRecorder, *http.Request) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	return w, req
}

func TestRedirect(t *testing.T) {
	w, req := newRedirectRequest(t)
	Redirect(w, req, "/new", http.StatusFound)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}

func TestRedirectCtx(t *testing.T) {
	w, req := newRedirectRequest(t)
	RedirectCtx(context.Background(), w, req, "/new", http.StatusMovedPermanently)
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}

func TestRedirectTemporary(t *testing.T) {
	w, req := newRedirectRequest(t)
	RedirectTemporary(w, req, "/new")
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}

func TestRedirectTemporaryCtx(t *testing.T) {
	w, req := newRedirectRequest(t)
	RedirectTemporaryCtx(context.Background(), w, req, "/new")
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}

func TestRedirectPermanent(t *testing.T) {
	w, req := newRedirectRequest(t)
	RedirectPermanent(w, req, "/new")
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}

func TestRedirectPermanentCtx(t *testing.T) {
	w, req := newRedirectRequest(t)
	RedirectPermanentCtx(context.Background(), w, req, "/new")
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/new", w.Header().Get("Location"))
}
