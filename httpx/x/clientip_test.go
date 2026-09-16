package x

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// trustProxy builds a request whose direct peer is a trusted proxy (127.0.0.1).
// Proxy headers are ignored when the direct peer is untrusted, so cases that involve
// "proxy header parsing" must first land on a trusted peer.
func trustProxy(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

// --- ClientIP: direct connection (no proxy header / untrusted direct peer) ---

func TestClientIP_RemoteAddr(t *testing.T) {
	// with no proxy header it falls back to RemoteAddr, with the port stripped
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	assert.Equal(t, "1.2.3.4", ClientIP(r))

	// IPv6 with a port
	r.RemoteAddr = "[2001:db8::1]:5678"
	assert.Equal(t, "2001:db8::1", ClientIP(r))

	// RemoteAddr without a port: returned as is
	r.RemoteAddr = "1.2.3.4"
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_UntrustedDirectIgnoresProxyHeaders(t *testing.T) {
	// the direct peer is a public (untrusted) address, so it counts as the real client
	// connecting directly: even a forged proxy header must be ignored, trusting only RemoteAddr.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set(HeaderXForwardedFor, "5.5.5.5, 6.6.6.6")
	r.Header.Set(HeaderXRealIP, "7.7.7.7")
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_EmptyAndNil(t *testing.T) {
	// no trustworthy source
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = ""
	assert.Equal(t, "", ClientIP(r))

	assert.Equal(t, "", ClientIP(nil))
}

// --- ClientIP: behind a trusted proxy (default resolver) ---

func TestClientIP_XRealIP(t *testing.T) {
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXRealIP, "5.5.5.5")
	assert.Equal(t, "5.5.5.5", ClientIP(r))
}

func TestClientIP_XForwardedFor_FromRight(t *testing.T) {
	// behind a trusted proxy, X-Forwarded-For is walked from right to left past the trusted
	// proxies to the first untrusted IP.
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "203.0.113.9, 70.41.3.18, 150.172.238.178")
	assert.Equal(t, "150.172.238.178", ClientIP(r))

	// X-Forwarded-For takes precedence over X-Real-IP
	r.Header.Set(HeaderXRealIP, "5.5.5.5")
	assert.Equal(t, "150.172.238.178", ClientIP(r))
}

func TestClientIP_XForwardedFor_Antiforgery(t *testing.T) {
	// a malicious client forged the leftmost prefix (1.1.1.1 is forged): secure resolution must
	// pick the real client 203.0.113.9 rather than the forged leftmost value.
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "1.1.1.1, 203.0.113.9")
	assert.Equal(t, "203.0.113.9", ClientIP(r))
}

func TestClientIP_XForwardedFor_SkipTrustedHops(t *testing.T) {
	// the XFF chain carries a trusted internal hop (10.0.0.5 appended by a trusted proxy);
	// trusted entries are skipped from right to left.
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "203.0.113.9, 10.0.0.5")
	assert.Equal(t, "203.0.113.9", ClientIP(r))

	// the whole chain consists of trusted proxies: take the leftmost (most original) value.
	r.Header.Set(HeaderXForwardedFor, "10.0.0.5, 10.0.0.6")
	assert.Equal(t, "10.0.0.5", ClientIP(r))
}

func TestClientIP_Forwarded(t *testing.T) {
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderForwarded, "for=203.0.113.60;proto=https;host=example.com")
	assert.Equal(t, "203.0.113.60", ClientIP(r))

	// quoted, with bracketed IPv6
	r.Header.Set(HeaderForwarded, `for="[2001:db8::1]:4704";proto=https`)
	assert.Equal(t, "2001:db8::1", ClientIP(r))

	// non-IP values such as for=unknown are ignored, falling back to the trusted RemoteAddr peer
	r.Header.Set(HeaderForwarded, "for=unknown;proto=http")
	assert.Equal(t, "127.0.0.1", ClientIP(r))
}

// --- ClientIPWithTrustedProxies: adding trusted CIDRs ---

func TestClientIPWithTrustedProxies(t *testing.T) {
	// traffic comes back through a public CDN (203.0.113.0/24): once the CDN egress range is
	// added as trusted, it is skipped from right to left to reach the more original real client.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:8080" // CDN egress
	r.Header.Set(HeaderXForwardedFor, "8.8.8.8, 203.0.113.5")
	// without the trusted CIDR: 203.0.113.10 is treated as the direct client
	assert.Equal(t, "203.0.113.10", ClientIP(r))
	// after adding the CDN range: the more original client is identified
	assert.Equal(t, "8.8.8.8", ClientIPWithTrustedProxies(r, "203.0.113.0/24"))
}

// --- IPChecker: custom vendor headers ---

func TestIPChecker_VendorHeaders(t *testing.T) {
	cf := NewIPChecker(WithVendorHeaders(HeaderCFConnectingIP))

	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderCFConnectingIP, "7.7.7.7")
	r.Header.Set(HeaderXForwardedFor, "9.9.9.9")
	// with the CF header enabled: CF-Connecting-IP is returned first
	assert.Equal(t, "7.7.7.7", cf.ClientIP(r))
	// the default resolver has vendor headers disabled: CF-Connecting-IP is ignored, XFF is used
	assert.Equal(t, "9.9.9.9", ClientIP(r))

	// vendor headers are just as untrustworthy when the direct peer is untrusted
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:80"
	r2.Header.Set(HeaderCFConnectingIP, "7.7.7.7")
	assert.Equal(t, "8.8.8.8", cf.ClientIP(r2))
}
