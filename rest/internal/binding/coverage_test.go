package binding

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requiredItem struct {
	Name string `binding:"required"`
}

func TestDefaultValidator_ValidateStruct(t *testing.T) {
	v := &defaultValidator{}

	assert.NoError(t, v.ValidateStruct(nil))
	assert.NoError(t, v.ValidateStruct(42))
	assert.NoError(t, v.ValidateStruct("scalar"))

	scalar := 7
	assert.NoError(t, v.ValidateStruct(&scalar), "pointer to non-struct recurses into the scalar")

	var nilStruct *requiredItem
	assert.NoError(t, v.ValidateStruct(nilStruct))

	require.Error(t, v.ValidateStruct(requiredItem{}))
	assert.NoError(t, v.ValidateStruct(requiredItem{Name: "ok"}))

	require.Error(t, v.ValidateStruct([]requiredItem{{Name: "ok"}, {}}))
	require.Error(t, v.ValidateStruct([1]requiredItem{{}}))
	assert.NoError(t, v.ValidateStruct([]requiredItem{{Name: "a"}, {Name: "b"}}))

	engine, ok := v.Engine().(*validator.Validate)
	require.True(t, ok)
	assert.NotNil(t, engine)
}

func TestSliceValidationError(t *testing.T) {
	err := SliceValidationError{nil, errors.New("bad")}
	assert.Contains(t, err.Error(), "[1]")

	empty := SliceValidationError{}
	assert.Equal(t, "", empty.Error())
}

func TestBadRequestErrNil(t *testing.T) {
	assert.NoError(t, badRequestErr(nil))
}

func TestBinderNamesCoverage(t *testing.T) {
	assert.Equal(t, "json", JSON.Name())
	assert.Equal(t, "xml", XML.Name())
	assert.Equal(t, "form", Form.Name())
	assert.Equal(t, "form-urlencoded", FormPost.Name())
	assert.Equal(t, "multipart/form-data", FormMultipart.Name())
	assert.Equal(t, "query", Query.Name())
	assert.Equal(t, "header", Header.Name())
	assert.Equal(t, "path", Path.Name())
	assert.Equal(t, "form-body", formBodyBinding{}.Name())
}

func TestQueryAndPathBindRejectNilRequest(t *testing.T) {
	var out struct{}
	require.Error(t, Query.Bind(nil, &out))
	require.Error(t, Path.Bind(nil, &out))
}

func TestXMLBindBody(t *testing.T) {
	var out struct {
		Name string `xml:"name"`
	}
	require.NoError(t, XML.BindBody([]byte("<root><name>a</name></root>"), &out))
	assert.Equal(t, "a", out.Name)

	require.Error(t, XML.BindBody([]byte("<root>"), &out))
	require.Error(t, XML.BindBody([]byte("<a></a><b></b>"), &out))
}

func TestMultipartRejectsNonMultipart(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=ada"))
	req.Header.Set("Content-Type", MIMEPOSTForm)
	require.Error(t, FormMultipart.Bind(req, &struct{}{}))
}

func TestFieldKeyFallback(t *testing.T) {
	type noTag struct {
		Name    string
		Skipped string `form:"-"`
	}
	structType := reflect.TypeOf(noTag{})
	key, ok := fieldKey(structType.Field(0), "form")
	assert.True(t, ok)
	assert.Equal(t, "Name", key)

	_, ok = fieldKey(structType.Field(1), "form")
	assert.False(t, ok)
}

func TestReadLimitedBody(t *testing.T) {
	body, err := readLimitedBody(strings.NewReader("abc"), 3)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(body))

	_, err = readLimitedBody(strings.NewReader("abcd"), 3)
	require.Error(t, err)
	var bindErr *Error
	require.True(t, errors.As(err, &bindErr))
	assert.Equal(t, http.StatusRequestEntityTooLarge, bindErr.StatusCode())
}

func TestQueryValuesNilRequest(t *testing.T) {
	assert.Empty(t, queryValues(nil))
}

func TestFormBodyBinding_MergesQueryAndBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?name=query&limit=5", strings.NewReader("name=body&limit=3"))
	var out formPayload
	require.NoError(t, formBodyBinding{}.Bind(req, &out))
	assert.Equal(t, "body", out.Name)
	assert.Equal(t, 3, out.Limit)
}
