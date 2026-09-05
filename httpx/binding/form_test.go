package binding

import (
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 form.go：FormBinding（包含 query 和 post form）。

type formReq struct {
	Username string `form:"username" binding:"required"`
	Password string `form:"password" binding:"required"`
	Remember bool   `form:"remember"`
}

func TestForm_Bind_QueryAndBody(t *testing.T) {
	// query + urlencoded body 合并绑定
	req := httptest.NewRequest(http.MethodPost, "/?remember=true", strings.NewReader("username=admin&password=123456"))
	req.Header.Set("Content-Type", MIMEPOSTForm)

	var f formReq
	require.NoError(t, Form.Bind(req, &f))
	assert.Equal(t, "admin", f.Username)
	assert.Equal(t, "123456", f.Password)
	assert.True(t, f.Remember)
}

func TestForm_Bind_URLEncoded(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("username=admin&password=123456"))
	req.Header.Set("Content-Type", MIMEPOSTForm)

	var f formReq
	require.NoError(t, Form.Bind(req, &f))
	assert.Equal(t, "admin", f.Username)
}

func TestForm_ValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("password=123"))
	req.Header.Set("Content-Type", MIMEPOSTForm)

	var f formReq
	require.Error(t, Form.Bind(req, &f)) // username 缺失
}

func TestForm_Multipart(t *testing.T) {
	var body strings.Builder
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("username", "admin"))
	require.NoError(t, mw.WriteField("password", "secret"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var f formReq
	require.NoError(t, Form.Bind(req, &f))
	assert.Equal(t, "admin", f.Username)
	assert.Equal(t, "secret", f.Password)
}
