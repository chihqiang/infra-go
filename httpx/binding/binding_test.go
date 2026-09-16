package binding

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Covers binding.go: MIME constants, binder interfaces, built-in instances, Default selector.

// --- Interface implementation assertions (compile time) ---

var (
	_ Binding     = JSON // BindingBody → Binding
	_ Binding     = XML
	_ Binding     = Form
	_ Binding     = Query
	_ Binding     = Header
	_ BindingBody = JSON
	_ BindingBody = XML
	_ BindingUri  = Uri // Uri only implements BindingUri (without Bind)
)

// --- MIME constants ---

func TestMIMEConstants(t *testing.T) {
	assert.Equal(t, "application/json", MIMEJSON)
	assert.Equal(t, "application/xml", MIMEXML)
	assert.Equal(t, "text/xml", MIMEXML2)
	assert.Equal(t, "text/plain", MIMEPlain)
	assert.Equal(t, "application/x-www-form-urlencoded", MIMEPOSTForm)
	assert.Equal(t, "multipart/form-data", MIMEMultipartPOSTForm)
}

// --- Built-in instance names ---

func TestBuiltinBinders_Name(t *testing.T) {
	assert.Equal(t, "json", JSON.Name())
	assert.Equal(t, "xml", XML.Name())
	assert.Equal(t, "form", Form.Name())
	assert.Equal(t, "query", Query.Name())
	assert.Equal(t, "header", Header.Name())
	assert.Equal(t, "uri", Uri.Name())
}

// --- Default binder selection ---

func TestDefault_Selector(t *testing.T) {
	// GET always returns Form
	assert.Equal(t, "form", Default(http.MethodGet, "").Name())
	assert.Equal(t, "form", Default(http.MethodGet, MIMEJSON).Name())

	// Matched by Content-Type
	assert.Equal(t, "json", Default(http.MethodPost, MIMEJSON).Name())
	assert.Equal(t, "xml", Default(http.MethodPost, MIMEXML).Name())
	assert.Equal(t, "xml", Default(http.MethodPost, MIMEXML2).Name())
	assert.Equal(t, "form", Default(http.MethodPost, MIMEPOSTForm).Name())
	assert.Equal(t, "form", Default(http.MethodPost, MIMEMultipartPOSTForm).Name())
}

func TestDefault_ContentTypeEdge(t *testing.T) {
	// With parameters (; charset=utf-8)
	assert.Equal(t, "json", Default(http.MethodPost, "application/json; charset=utf-8").Name())
	assert.Equal(t, "xml", Default(http.MethodPost, "application/xml; charset=utf-8").Name())
	// Case-insensitive
	assert.Equal(t, "json", Default(http.MethodPost, "Application/JSON").Name())
	// Unparseable / unknown type → Form
	assert.Equal(t, "form", Default(http.MethodPost, "not-a-valid-mime;;").Name())
	assert.Equal(t, "form", Default(http.MethodPost, "application/unknown").Name())
	assert.Equal(t, "form", Default(http.MethodPost, "").Name())
}
