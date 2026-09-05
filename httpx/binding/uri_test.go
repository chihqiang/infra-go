package binding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 uri.go：URIBinding。

type uriReq struct {
	ID   int    `uri:"id" binding:"required"`
	Slug string `uri:"slug"`
}

func TestURI_Bind(t *testing.T) {
	var u uriReq
	require.NoError(t, Uri.BindUri(map[string][]string{
		"id":   {"123"},
		"slug": {"hello-world"},
	}, &u))
	assert.Equal(t, 123, u.ID)
	assert.Equal(t, "hello-world", u.Slug)
}

func TestURI_Bind_MapString(t *testing.T) {
	// BindURI 风格：map[string]string → map[string][]string
	m := make(map[string][]string, 1)
	for k, v := range map[string]string{"id": "7"} {
		m[k] = []string{v}
	}
	var u uriReq
	require.NoError(t, Uri.BindUri(m, &u))
	assert.Equal(t, 7, u.ID)
}

func TestURI_ValidationError(t *testing.T) {
	var u uriReq
	require.Error(t, Uri.BindUri(map[string][]string{"slug": {"x"}}, &u)) // 缺 id
}
