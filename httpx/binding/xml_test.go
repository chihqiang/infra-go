package binding

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 xml.go：XMLBinding。

type xmlReq struct {
	Name  string `xml:"name" binding:"required"`
	Email string `xml:"email" binding:"required,email"`
}

func newXMLReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", MIMEXML)
	return req
}

func TestXML_Bind(t *testing.T) {
	req := newXMLReq(`<xmlReq><name>Bob</name><email>bob@example.com</email></xmlReq>`)
	var u xmlReq
	require.NoError(t, XML.Bind(req, &u))
	assert.Equal(t, "Bob", u.Name)
	assert.Equal(t, "bob@example.com", u.Email)
}

func TestXML_BindBody(t *testing.T) {
	body := []byte(`<xmlReq><name>Bob</name><email>bob@example.com</email></xmlReq>`)
	var u xmlReq
	require.NoError(t, XML.BindBody(body, &u))
	assert.Equal(t, "Bob", u.Name)
}

func TestXML_ValidationError(t *testing.T) {
	body := []byte(`<xmlReq><name>Bob</name></xmlReq>`) // 缺 email
	var u xmlReq
	require.Error(t, XML.BindBody(body, &u))
}

func TestXML_InvalidXML(t *testing.T) {
	var u xmlReq
	require.Error(t, XML.BindBody([]byte(`<xmlReq><name>`), &u))
}

func TestXML_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	var u xmlReq
	require.Error(t, XML.Bind(req, &u))
}

func TestXML_NilRequest(t *testing.T) {
	var u xmlReq
	require.Error(t, XML.Bind(nil, &u))
}
