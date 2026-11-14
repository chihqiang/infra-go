package realip

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestFromRequestPrefersForwardedFor(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.7, 70.41.3.18")

	if got := FromRequest(request); got != "203.0.113.7" {
		t.Fatalf("ip = %q, want the leftmost forwarded address", got)
	}
}

func TestFromRequestFallsBackToRealIP(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Real-IP", "198.51.100.9")

	if got := FromRequest(request); got != "198.51.100.9" {
		t.Fatalf("ip = %q, want 198.51.100.9", got)
	}
}

func TestFromRequestIgnoresInvalidProxyHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "not-an-ip")
	request.Header.Set("X-Real-IP", "also-bad")

	if got := FromRequest(request); got != "10.0.0.1" {
		t.Fatalf("ip = %q, want remote host 10.0.0.1", got)
	}
}

func TestFromRequestHandlesNil(t *testing.T) {
	if got := FromRequest(nil); got != "" {
		t.Fatalf("ip = %q, want empty", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	prefixes, err := ParseTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", " ", "2001:db8::/32"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 3 {
		t.Fatalf("prefixes = %v, want 3 entries", prefixes)
	}
	if !prefixes[0].Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Fatalf("prefix[0] = %v, want 127.0.0.1/32", prefixes[0])
	}
	if prefixes[1].Bits() != 8 {
		t.Fatalf("prefix[1] = %v, want /8", prefixes[1])
	}

	if _, err := ParseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Fatal("invalid trusted proxy should fail")
	}
	if _, err := ParseTrustedProxies([]string{"10.0.0.0/33"}); err == nil {
		t.Fatal("invalid CIDR should fail")
	}
}

func TestFromRequestTrustedOnlyUsesHeadersFromTrustedPeer(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	spoofed := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofed.RemoteAddr = "203.0.113.50:1234"
	spoofed.Header.Set("X-Forwarded-For", "1.2.3.4")
	spoofed.Header.Set("X-Real-IP", "5.6.7.8")
	if got := FromRequestTrusted(spoofed, trusted); got != "203.0.113.50" {
		t.Fatalf("untrusted peer ip = %q, want 203.0.113.50", got)
	}

	trustedPeer := httptest.NewRequest(http.MethodGet, "/", nil)
	trustedPeer.RemoteAddr = "10.0.0.1:1234"
	trustedPeer.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := FromRequestTrusted(trustedPeer, trusted); got != "203.0.113.7" {
		t.Fatalf("trusted peer ip = %q, want 203.0.113.7", got)
	}
}

func TestFromRequestTrustedWalksForwardedChain(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	// The leftmost entry is client-controlled; the rightmost untrusted hop is
	// the real client.
	request.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.7, 10.0.0.3")
	if got := FromRequestTrusted(request, trusted); got != "203.0.113.7" {
		t.Fatalf("ip = %q, want 203.0.113.7", got)
	}
}

func TestFromRequestTrustedFallsBackToRealIP(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Real-IP", "198.51.100.9")
	if got := FromRequestTrusted(request, trusted); got != "198.51.100.9" {
		t.Fatalf("ip = %q, want 198.51.100.9", got)
	}
}

func TestFromRequestTrustedEmptyIgnoresForwardingHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.50:1234"
	request.Header.Set("X-Forwarded-For", "1.2.3.4")
	request.Header.Set("X-Real-IP", "5.6.7.8")

	if got := FromRequestTrusted(request, nil); got != "203.0.113.50" {
		t.Fatalf("ip = %q, want the direct peer address", got)
	}
	if got := FromRequestTrusted(nil, nil); got != "" {
		t.Fatalf("nil request ip = %q, want empty", got)
	}
}
