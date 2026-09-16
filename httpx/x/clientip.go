// Real client IP resolution, aimed at services "deployed behind a trusted
// reverse proxy".
//
// It is unrelated to the middleware itself and belongs to the generic httpx
// utilities (gathered in this x package), so it can be reused and called at high
// frequency per IP (rate limiting/auditing). See the IPChecker / ClientIP docs.
package x

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// --- Common proxy header constants ---

const (
	// HeaderXForwardedFor is X-Forwarded-For: reverse proxies append the client IP
	// along the forwarding chain, with multiple values separated by commas; the
	// leftmost entry is the original client and the rightmost is the hop closest
	// to this service.
	HeaderXForwardedFor = "X-Forwarded-For"

	// HeaderXRealIP is X-Real-IP: a single-layer proxy (such as Nginx with
	// "proxy_set_header X-Real-IP $remote_addr") writes the real client IP here
	// directly.
	HeaderXRealIP = "X-Real-IP"

	// HeaderForwarded is the RFC 7239 standardized forwarding header, of the form:
	//   Forwarded: for=203.0.113.7;proto=https;host=example.com
	// The for parameter may be quoted (for="1.2.3.4") or use the bracketed IPv6
	// form (for="[2001:db8::1]:4704").
	HeaderForwarded = "Forwarded"

	// HeaderCFConnectingIP is the client IP header Cloudflare writes/overwrites on
	// origin requests.
	HeaderCFConnectingIP = "CF-Connecting-IP"

	// HeaderTrueClientIP is the client IP header used by some CDNs such as Akamai.
	HeaderTrueClientIP = "True-Client-IP"
)

// trustCIDRs holds the networks treated as trusted proxies by default: loopback,
// private, link-local and unique local addresses.
// These addresses never appear on the public internet and usually come from a
// local proxy, an intranet load balancer or a gateway forwarding origin requests;
// if traffic reaches the origin through a public CDN/WAF/cloud LB, use
// WithTrustedProxies to add its egress networks.
var trustCIDRs = sync.OnceValue(func() []*net.IPNet {
	return mustCIDRs([]string{
		"127.0.0.0/8",    // IPv4 loopback
		"::1/128",        // IPv6 loopback
		"10.0.0.0/8",     // private
		"172.16.0.0/12",  // private
		"192.168.0.0/16", // private
		"169.254.0.0/16", // IPv4 link-local
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local
	})
})

// IPChecker resolves the real client IP for services "deployed behind a trusted
// reverse proxy".
//
// The decision process starts from the directly connected peer (RemoteAddr).
//   - Untrusted peer (a real client connecting directly over the public
//     internet) -> proxy headers are not trusted at all, the peer is returned;
//   - Trusted peer (inside a trusted network, such as localhost/an intranet
//     gateway/a load balancer) -> only then are proxy headers parsed to obtain
//     the real client.
//
// Proxy header resolution order:
//  1. Vendor headers (such as CF-Connecting-IP / True-Client-IP, which must be
//     enabled explicitly via WithVendorHeaders);
//  2. X-Forwarded-For: walk from right to left skipping trusted proxies and take
//     the first untrusted IP (defeats forged prefixes);
//  3. Forwarded (RFC 7239): take the for= parameter;
//  4. X-Real-IP;
//  5. Fall back to RemoteAddr.
type IPChecker struct {
	trusted []*net.IPNet // additional trusted proxy networks
	vendor  []string     // enabled vendor single-value headers (before X-Forwarded-For)
}

// IPOption configures an IPChecker.
type IPOption func(*IPChecker)

// WithTrustedProxies adds trusted proxy networks (CIDR or bare IP).
// Loopback and private networks are already trusted by default; if traffic
// reaches the origin through a public CDN/WAF/cloud LB, add its egress networks,
// otherwise resolution stops at those proxies (the more original client IP cannot
// be obtained).
func WithTrustedProxies(cidrs ...string) IPOption {
	return func(c *IPChecker) {
		c.trusted = append(c.trusted, mustCIDRs(cidrs)...)
	}
}

// WithVendorHeaders enables/customizes vendor client IP headers (such as
// HeaderCFConnectingIP, HeaderTrueClientIP). Those headers take precedence over
// X-Forwarded-For when the peer is trusted and the header is present.
// They should only be enabled when traffic is known to pass through the matching
// vendor (such as Cloudflare/Akamai); passing no arguments disables vendor
// headers.
func WithVendorHeaders(headers ...string) IPOption {
	return func(c *IPChecker) {
		c.vendor = headers
	}
}

// NewIPChecker creates a client IP resolver; options customize the trusted
// networks and the vendor headers:
//
//	ipc := NewIPChecker(
//		WithTrustedProxies("100.64.0.0/10"),     // add a trusted gateway network
//		WithVendorHeaders(HeaderCFConnectingIP), // enable the Cloudflare header
//	)
//	remote := ipc.ClientIP(r)
//
// With no options it is equivalent to NewIPChecker(): loopback/private addresses
// are trusted, X-Forwarded-For / Forwarded / X-Real-IP are recognized, and no
// vendor header is enabled.
// Parsing rules are precompiled (trusted networks are parsed once), making it
// suitable for high-frequency per-request reuse (such as per-IP rate limiting).
func NewIPChecker(opts ...IPOption) *IPChecker {
	c := &IPChecker{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ClientIP resolves the real client IP of the request (a bare IP, without the
// port). See the IPChecker docs for the rules. It returns an empty string when r
// is nil or the IP cannot be determined.
func (c *IPChecker) ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	direct := remoteIP(r.RemoteAddr)
	if direct == "" {
		return ""
	}
	// Untrusted direct peer: it is treated as a real client connecting directly,
	// so proxy headers are not trusted at all.
	if !c.isTrusted(direct) {
		return direct
	}

	// 1. Vendor headers (returned as soon as one is present; usually more
	// trustworthy than XFF, which the vendor overwrites)
	for _, h := range c.vendor {
		if ip := strings.TrimSpace(r.Header.Get(h)); ip != "" {
			return ip
		}
	}
	// 2. X-Forwarded-For: walk from right to left skipping trusted proxies and
	// take the first untrusted IP
	if ip := c.xffIP(r); ip != "" {
		return ip
	}
	// 3. Forwarded (RFC 7239)
	if ip := forwardedIP(r.Header.Get(HeaderForwarded)); ip != "" {
		return ip
	}
	// 4. X-Real-IP
	if ip := strings.TrimSpace(r.Header.Get(HeaderXRealIP)); ip != "" {
		return ip
	}
	return direct
}

// defaultIPChecker is the package-level default resolver: loopback/private
// addresses are trusted, XFF/Forwarded/X-Real-IP are recognized, no vendor header
// is enabled.
var defaultIPChecker = NewIPChecker()

// ClientIP returns the real client IP of the request (a bare IP, without the
// port). It uses the default trust rules (loopback and private addresses count as
// trusted proxies) and safe parsing (see the IPChecker docs), which resists
// forged prefixes unlike "blindly trusting the leftmost X-Forwarded-For value".
// Use NewIPChecker to build a resolver when custom trusted networks or vendor
// headers are needed.
func ClientIP(r *http.Request) string {
	return defaultIPChecker.ClientIP(r)
}

// ClientIPWithTrustedProxies resolves the client IP after adding custom trusted
// proxy networks on top of the default ones.
// It fits traffic that reaches the origin through a public CDN/WAF/cloud LB whose
// egress networks are outside the default intranet ranges.
// To reuse a resolver (avoiding re-parsing the networks on every call), use
// NewIPChecker(WithTrustedProxies(...)) instead.
func ClientIPWithTrustedProxies(r *http.Request, trusted ...string) string {
	return NewIPChecker(WithTrustedProxies(trusted...)).ClientIP(r)
}

// isTrusted reports whether ipStr falls inside a trusted proxy network (one of
// the default networks or an additional one).
func (c *IPChecker) isTrusted(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	for _, n := range trustCIDRs() {
		if n.Contains(ip) {
			return true
		}
	}
	for _, n := range c.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// xffIP resolves the real client from X-Forwarded-For: it iterates from right to
// left (the hop closest to this service), skipping trusted proxies, and the first
// untrusted IP is the client; when the whole chain consists of trusted proxies it
// takes the leftmost (most original) value.
func (c *IPChecker) xffIP(r *http.Request) string {
	xff := r.Header.Get(HeaderXForwardedFor)
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !c.isTrusted(ip) {
			return ip
		}
	}
	// The whole chain consists of trusted proxies (or contains unparsable entries):
	// take the leftmost non-empty value (the most original client)
	for _, p := range parts {
		if ip := strings.TrimSpace(p); ip != "" {
			return ip
		}
	}
	return ""
}

// forwardedIP parses the first for= parameter of an RFC 7239 Forwarded header.
// Quoted and bracketed IPv6 forms are supported: for=1.2.3.4 /
// for="[2001:db8::1]:4704" / for=192.0.2.1:8080; non-IP values such as
// for=unknown / obfuscated yield an empty string.
func forwardedIP(h string) string {
	for _, part := range strings.Split(h, ";") {
		key, val, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(val), `"`)
		if v == "" || strings.EqualFold(v, "unknown") || strings.EqualFold(v, "obfuscated") {
			continue
		}
		if strings.HasPrefix(v, "[") {
			if end := strings.IndexByte(v, ']'); end > 0 {
				return v[1:end]
			}
		} else if strings.Contains(v, ":") {
			if host, _, err := net.SplitHostPort(v); err == nil {
				return host
			}
		}
		return v
	}
	return ""
}

// mustCIDRs parses a list of CIDR/IP strings into a set of networks; bare IPs are
// treated as fully masked networks (/32, /128).
func mustCIDRs(list []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, s := range list {
		out = append(out, mustCIDR(s))
	}
	return out
}

// mustCIDR parses a single CIDR/IP string into a network; when it cannot be
// parsed it returns 0.0.0.0/32 (which never matches), so a configuration mistake
// does not break the caller.
func mustCIDR(s string) *net.IPNet {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		_, n, err := net.ParseCIDR(s)
		if err == nil {
			return n
		}
	} else if ip := net.ParseIP(s); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
	}
	return &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(32, 32)}
}

// remoteIP strips the port from addr and returns a bare IP; addr is returned
// as-is when it has no port or cannot be parsed.
// Port forms such as "1.2.3.4:5678" and "2001:db8::1:5678" are supported, as are
// bare addresses without a port.
func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
