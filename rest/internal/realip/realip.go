// Package realip extracts the client address from a request.
package realip

import (
	"net"
	"net/http"
	"strings"
)

const (
	headerForwardedFor = "X-Forwarded-For"
	headerRealIP       = "X-Real-IP"
)

// FromRequest returns the client IP, preferring proxy headers when they hold a
// valid address. Ports are stripped when present.
func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	if forwarded := r.Header.Get(headerForwardedFor); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := normalize(first); ip != "" {
			return ip
		}
	}
	if ip := normalize(r.Header.Get(headerRealIP)); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
	}
	return ""
}
