package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Client IP (httpx package convenience entry points, forwarding to httpx/x) ---

func TestClientIP_RemoteAddrFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_ProxyHeaders(t *testing.T) {
	// Resolve proxy headers when the direct peer is a trusted proxy (loopback)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	// Spoof protection: skip the trusted right-hand side and take the real client
	assert.Equal(t, "203.0.113.9", ClientIP(r))

	r.Header.Set("X-Real-IP", "8.8.8.8")
	assert.Equal(t, "203.0.113.9", ClientIP(r))
}

func TestClientIP_Nil(t *testing.T) {
	assert.Equal(t, "", ClientIP(nil))
}

func TestClientIPWithTrustedProxies(t *testing.T) {
	// Traffic comes back through a public CDN (203.0.113.0/24): after appending the
	// trusted range, the more original client is returned
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:8080"
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 203.0.113.5")

	assert.Equal(t, "203.0.113.10", ClientIP(r)) // not appended: CDN egress is treated as direct
	assert.Equal(t, "8.8.8.8", ClientIPWithTrustedProxies(r, "203.0.113.0/24"))
}
