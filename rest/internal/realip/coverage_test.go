package realip

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeAndRemoteHost(t *testing.T) {
	assert.Equal(t, "192.0.2.1", normalize(" 192.0.2.1 "))
	assert.Equal(t, "192.0.2.2", normalize("192.0.2.2:8080"))
	assert.Equal(t, "", normalize("not-an-ip"))
	assert.Equal(t, "", normalize(""))

	assert.Equal(t, "192.0.2.3", remoteHost("192.0.2.3:1234"))
	assert.Equal(t, "unix-socket", remoteHost("unix-socket"))
}

func TestParseIPRejectsInvalid(t *testing.T) {
	_, ok := parseIP("invalid")
	assert.False(t, ok)
	addr, ok := parseIP("192.0.2.4")
	assert.True(t, ok)
	assert.Equal(t, "192.0.2.4", addr.String())
}

func TestFromRequestTrustedFallbacks(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	assert.Equal(t, "203.0.113.1", FromRequest(req))
	assert.Equal(t, "192.0.2.5", FromRequestTrusted(req, nil))

	// A trusted peer with an untrusted right-most entry returns that entry.
	trusted, err := ParseTrustedProxies([]string{"192.0.2.0/24"})
	assert.NoError(t, err)
	assert.Equal(t, "203.0.113.1", FromRequestTrusted(req, trusted))
}
