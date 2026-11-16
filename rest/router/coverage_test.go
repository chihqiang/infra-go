package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatternWildcardAndEmpty(t *testing.T) {
	assert.Equal(t, "/x", Pattern("*", "/x"))
	assert.Equal(t, "/x", Pattern("", "x"))
	assert.Equal(t, "/", Pattern("", ""))
	assert.Equal(t, "/", NormalizePath(""))
	assert.Equal(t, "/a/b/", JoinPath("/a", "b/"))
}

func TestIsNotFoundUsesStatusRecorder(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/known", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	require.True(t, r.IsNotFound(httptest.NewRequest(http.MethodGet, "/missing", nil)))
	assert.False(t, r.IsNotFound(httptest.NewRequest(http.MethodGet, "/known", nil)))
	assert.True(t, r.IsMethodNotAllowed(httptest.NewRequest(http.MethodPost, "/known", nil)))
	assert.False(t, r.IsNotFound(httptest.NewRequest(http.MethodPost, "/known", nil)))
}
