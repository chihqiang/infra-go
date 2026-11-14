package realip

import (
	"net/http"
	"net/http/httptest"
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
