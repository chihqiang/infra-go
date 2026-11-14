// Package realip extracts the client address from a request.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	headerForwardedFor = "X-Forwarded-For"
	headerRealIP       = "X-Real-IP"
)

// FromRequest returns the client IP, preferring proxy headers when they hold a
// valid address. Ports are stripped when present.
//
// The forwarding headers are trusted from any peer; use FromRequestTrusted when
// the server is reachable directly and those headers can be spoofed.
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
	return remoteHost(r.RemoteAddr)
}

// FromRequestTrusted is FromRequest that only honours X-Forwarded-For /
// X-Real-IP when the direct peer is inside trusted. The forwarded chain is
// walked from right to left, skipping trusted proxies, so client-supplied
// entries cannot spoof the address.
//
// An empty trusted list trusts no proxy: only the direct peer address is
// returned. Configure trusted proxies explicitly when running behind one.
func FromRequestTrusted(r *http.Request, trusted []netip.Prefix) string {
	if r == nil {
		return ""
	}
	if len(trusted) == 0 {
		return remoteHost(r.RemoteAddr)
	}

	peer, ok := parseIP(remoteHost(r.RemoteAddr))
	if !ok || !isTrusted(peer, trusted) {
		return remoteHost(r.RemoteAddr)
	}

	if forwarded := r.Header.Get(headerForwardedFor); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := normalize(parts[i])
			if ip == "" {
				continue
			}
			addr, err := netip.ParseAddr(ip)
			if err == nil && isTrusted(addr.Unmap(), trusted) {
				continue
			}
			return ip
		}
	}
	if ip := normalize(r.Header.Get(headerRealIP)); ip != "" {
		return ip
	}
	return remoteHost(r.RemoteAddr)
}

// ParseTrustedProxies parses IP and CIDR strings into prefixes. Invalid entries
// are reported instead of being silently ignored.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return nil, nil
	}

	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, fmt.Errorf("realip: invalid trusted proxy %q: %w", value, err)
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("realip: invalid trusted proxy %q: %w", value, err)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseIP(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
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
