// Package cors implements Cross-Origin Resource Sharing for the HTTP server.
package cors

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/chihqiang/ten-go/rest/internal/response"
)

const (
	headerOrigin           = "Origin"
	headerAllowOrigin      = "Access-Control-Allow-Origin"
	headerAllowMethods     = "Access-Control-Allow-Methods"
	headerAllowHeaders     = "Access-Control-Allow-Headers"
	headerAllowCredentials = "Access-Control-Allow-Credentials"
	headerExposeHeaders    = "Access-Control-Expose-Headers"
	headerMaxAge           = "Access-Control-Max-Age"
	headerRequestMethod    = "Access-Control-Request-Method"
	headerRequestHeaders   = "Access-Control-Request-Headers"
	headerVary             = "Vary"

	allowAnyOrigin = "*"
	allowMethods   = "GET, HEAD, POST, PATCH, PUT, DELETE"
	allowHeaders   = "Content-Type, Origin, X-CSRF-Token, Authorization, AccessToken, Token, Range"
	exposeHeaders  = "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers"
	allowTrue      = "true"
	maxAgeSeconds  = "86400"
)

// AddAllowHeaders appends extra allowed request headers to the CORS response.
func AddAllowHeaders(header http.Header, headers ...string) {
	if len(headers) == 0 {
		return
	}
	header.Add(headerAllowHeaders, strings.Join(headers, ", "))
}

// Middleware adds CORS headers to responses. With no origins configured, all
// origins are allowed without credentials. A nil headerFn is ignored.
//
// Only a real preflight (OPTIONS with both Origin and
// Access-Control-Request-Method) is answered locally; plain OPTIONS requests
// still reach the route, so handlers can implement their own OPTIONS semantics.
func Middleware(headerFn func(http.Header, *http.Request), origins ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			writeHeaders(w, r, origins)
			if headerFn != nil {
				headerFn(w.Header(), r)
			}

			if isPreflight(r) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next(w, r)
		}
	}
}

// NotAllowedHandler answers requests rejected before reaching a route, such as
// unmatched paths and preflight requests, while still applying CORS headers.
func NotAllowedHandler(fn func(http.ResponseWriter), origins ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer := response.NewDeferredWriter(w)
		writeHeaders(writer, r, origins)
		if fn != nil {
			fn(writer)
		}

		status := http.StatusNotFound
		if isPreflight(r) {
			status = http.StatusNoContent
		}
		writer.Flush(status)
	})
}

// IsPreflight reports whether r is a CORS preflight request: an OPTIONS request
// that announces both the requesting origin and the intended method.
func IsPreflight(r *http.Request) bool {
	return isPreflight(r)
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions &&
		r.Header.Get(headerOrigin) != "" &&
		r.Header.Get(headerRequestMethod) != ""
}

func writeHeaders(w http.ResponseWriter, r *http.Request, origins []string) {
	setVary(w, r)

	origin := allowedOrigin(origins, r.Header.Get(headerOrigin))
	if origin == "" {
		return
	}

	header := w.Header()
	header.Set(headerAllowOrigin, origin)
	header.Set(headerExposeHeaders, exposeHeaders)
	// Methods, allowed request headers and max-age are only meaningful on a
	// preflight response; keep them off ordinary responses.
	if isPreflight(r) {
		header.Set(headerAllowMethods, allowMethods)
		header.Set(headerAllowHeaders, allowHeaders)
		header.Set(headerMaxAge, maxAgeSeconds)
	}
	if origin == allowAnyOrigin {
		header.Del(headerAllowCredentials)
	} else {
		header.Set(headerAllowCredentials, allowTrue)
	}
}

func setVary(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	addVary(header, headerOrigin)
	if r.Method == http.MethodOptions {
		addVary(header, headerRequestMethod)
		addVary(header, headerRequestHeaders)
	}
}

// addVary appends value unless it is already present, so CORS running both as
// middleware and as the not-found fallback does not duplicate Vary entries.
func addVary(header http.Header, value string) {
	for _, existing := range header.Values(headerVary) {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add(headerVary, value)
}

func allowedOrigin(origins []string, origin string) string {
	if len(origins) == 0 {
		return allowAnyOrigin
	}

	for _, allowed := range origins {
		if allowed == allowAnyOrigin {
			return allowAnyOrigin
		}
		if matchesOrigin(allowed, origin) {
			return origin
		}
	}
	return ""
}

func matchesOrigin(allowed, origin string) bool {
	allowed = normalizeOrigin(allowed)
	origin = normalizeOrigin(origin)
	if allowed == "" || origin == "" {
		return false
	}
	if origin == allowed {
		return true
	}
	// Entries with a scheme are matched exactly (case-insensitively). Subdomain
	// wildcards require the scheme-less host form, e.g. "example.com" matches
	// "https://api.example.com" but not an unrelated host.
	if strings.Contains(allowed, "://") {
		return false
	}
	// Refuse bare top-level labels such as "com": the suffix check below would
	// otherwise allow every ".com" origin.
	if !strings.Contains(allowed, ".") {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	return host == allowed || strings.HasSuffix(host, "."+allowed)
}

// normalizeOrigin makes origin comparisons case-insensitive and ignores a
// single trailing slash.
func normalizeOrigin(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "/"))
}
