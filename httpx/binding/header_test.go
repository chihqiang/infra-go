package binding

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers header.go: HeaderBinding (including the canonical key conversion of headerSource.TrySet).

type headerReq struct {
	Token   string `header:"X-Token" binding:"required"`
	Version string `header:"X-Version,default=v1"`
}

func newHeaderReq(set map[string]string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	for k, v := range set {
		req.Header.Set(k, v)
	}
	return req
}

func TestHeader_Bind(t *testing.T) {
	var h headerReq
	require.NoError(t, Header.Bind(newHeaderReq(map[string]string{"X-Token": "abc"}), &h))
	assert.Equal(t, "abc", h.Token)
	assert.Equal(t, "v1", h.Version) // falls back to default when absent
}

func TestHeader_CaseInsensitive(t *testing.T) {
	// headerSource.TrySet converts the key to canonical MIME form, so lookups are case-insensitive
	var h headerReq
	require.NoError(t, Header.Bind(newHeaderReq(map[string]string{"x-token": "abc"}), &h))
	assert.Equal(t, "abc", h.Token)
}

func TestHeader_ValidationError(t *testing.T) {
	var h headerReq
	require.Error(t, Header.Bind(newHeaderReq(nil), &h)) // X-Token is missing
}
