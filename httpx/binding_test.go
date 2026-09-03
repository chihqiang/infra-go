package httpx

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BindBody 测试 ---

func TestJSON_BindBody(t *testing.T) {
	body := []byte(`{"name":"Alice","age":25,"email":"alice@example.com"}`)

	var user userRequest
	err := JSON.BindBody(body, &user)
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

func TestDefault_PostJSON_WithCharset(t *testing.T) {
	b := Default(http.MethodPost, "application/json; charset=utf-8")
	assert.Equal(t, "json", b.Name())
}

func TestDefault_PostJSON_CaseInsensitive(t *testing.T) {
	b := Default(http.MethodPost, "Application/JSON")
	assert.Equal(t, "json", b.Name())
}

func TestDefault_PostXML_WithParam(t *testing.T) {
	b := Default(http.MethodPost, "application/xml; charset=utf-8")
	assert.Equal(t, "xml", b.Name())
}

func TestDefault_InvalidContentType(t *testing.T) {
	b := Default(http.MethodPost, "not-a-valid-mime;;")
	assert.Equal(t, "form", b.Name())
}
