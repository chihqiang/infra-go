package binding

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 对应 binding.go：MIME 常量、绑定器接口、内置实例、Default 选择器。

// --- 接口实现断言（编译期） ---

var (
	_ Binding     = JSON // BindingBody → Binding
	_ Binding     = XML
	_ Binding     = Form
	_ Binding     = Query
	_ Binding     = Header
	_ BindingBody = JSON
	_ BindingBody = XML
	_ BindingUri  = Uri // Uri 仅实现 BindingUri（不含 Bind）
)

// --- MIME 常量 ---

func TestMIMEConstants(t *testing.T) {
	assert.Equal(t, "application/json", MIMEJSON)
	assert.Equal(t, "application/xml", MIMEXML)
	assert.Equal(t, "text/xml", MIMEXML2)
	assert.Equal(t, "text/plain", MIMEPlain)
	assert.Equal(t, "application/x-www-form-urlencoded", MIMEPOSTForm)
	assert.Equal(t, "multipart/form-data", MIMEMultipartPOSTForm)
}

// --- 内置实例名称 ---

func TestBuiltinBinders_Name(t *testing.T) {
	assert.Equal(t, "json", JSON.Name())
	assert.Equal(t, "xml", XML.Name())
	assert.Equal(t, "form", Form.Name())
	assert.Equal(t, "query", Query.Name())
	assert.Equal(t, "header", Header.Name())
	assert.Equal(t, "uri", Uri.Name())
}

// --- Default 绑定器选择 ---

func TestDefault_Selector(t *testing.T) {
	// GET 固定返回 Form
	assert.Equal(t, "form", Default(http.MethodGet, "").Name())
	assert.Equal(t, "form", Default(http.MethodGet, MIMEJSON).Name())

	// 按 Content-Type 匹配
	assert.Equal(t, "json", Default(http.MethodPost, MIMEJSON).Name())
	assert.Equal(t, "xml", Default(http.MethodPost, MIMEXML).Name())
	assert.Equal(t, "xml", Default(http.MethodPost, MIMEXML2).Name())
	assert.Equal(t, "form", Default(http.MethodPost, MIMEPOSTForm).Name())
	assert.Equal(t, "form", Default(http.MethodPost, MIMEMultipartPOSTForm).Name())
}

func TestDefault_ContentTypeEdge(t *testing.T) {
	// 带参数（; charset=utf-8）
	assert.Equal(t, "json", Default(http.MethodPost, "application/json; charset=utf-8").Name())
	assert.Equal(t, "xml", Default(http.MethodPost, "application/xml; charset=utf-8").Name())
	// 大小写不敏感
	assert.Equal(t, "json", Default(http.MethodPost, "Application/JSON").Name())
	// 无法解析 / 未知类型 → Form
	assert.Equal(t, "form", Default(http.MethodPost, "not-a-valid-mime;;").Name())
	assert.Equal(t, "form", Default(http.MethodPost, "application/unknown").Name())
	assert.Equal(t, "form", Default(http.MethodPost, "").Name())
}
