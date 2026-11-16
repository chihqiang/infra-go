package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPreflight(t *testing.T) {
	preflight := httptest.NewRequest(http.MethodOptions, "/", nil)
	preflight.Header.Set("Origin", "https://example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	assert.True(t, IsPreflight(preflight))

	noMethod := httptest.NewRequest(http.MethodOptions, "/", nil)
	noMethod.Header.Set("Origin", "https://example.com")
	assert.False(t, IsPreflight(noMethod))

	notOptions := httptest.NewRequest(http.MethodGet, "/", nil)
	notOptions.Header.Set("Origin", "https://example.com")
	notOptions.Header.Set("Access-Control-Request-Method", http.MethodPost)
	assert.False(t, IsPreflight(notOptions))

	assert.False(t, IsPreflight(httptest.NewRequest(http.MethodOptions, "/", nil)))
}
