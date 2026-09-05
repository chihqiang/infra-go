package binding

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 json.go：JSONBinding。

type jsonReq struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age"`
}

func newJSONReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", MIMEJSON)
	return req
}

func TestJSON_Bind(t *testing.T) {
	req := newJSONReq(`{"name":"Alice","age":25,"email":"alice@example.com"}`)
	var u jsonReq
	require.NoError(t, JSON.Bind(req, &u))
	assert.Equal(t, "Alice", u.Name)
	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, 25, u.Age)
}

func TestJSON_BindBody(t *testing.T) {
	body := []byte(`{"name":"Alice","age":25,"email":"alice@example.com"}`)
	var u jsonReq
	require.NoError(t, JSON.BindBody(body, &u))
	assert.Equal(t, "Alice", u.Name)
	assert.Equal(t, 25, u.Age)
}

func TestJSON_ValidationError(t *testing.T) {
	var u jsonReq
	require.Error(t, JSON.BindBody([]byte(`{"name":"Alice"}`), &u)) // 缺 email
}

func TestJSON_InvalidJSON(t *testing.T) {
	var u jsonReq
	require.Error(t, JSON.BindBody([]byte(`{invalid`), &u))
}

func TestJSON_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	var u jsonReq
	require.Error(t, JSON.Bind(req, &u))
}

func TestJSON_NilRequest(t *testing.T) {
	var u jsonReq
	require.Error(t, JSON.Bind(nil, &u))
}
