package handler

import (
	"net/http"
	"net/url"
	"strings"
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

// CORSConfig configures CORSHandler. It can be loaded from a config file.
type CORSConfig struct {
	// Origins lists the allowed origins. An empty list (or "*") allows every
	// origin without credentials. Entries with a scheme match exactly; a
	// scheme-less host also allows its subdomains.
	Origins []string `json:",optional"`
	// Headers adds allowed request headers to preflight responses.
	Headers []string `json:",optional"`
}

// CORSHandler adds Cross-Origin Resource Sharing headers to responses.
type CORSHandler struct {
	config CORSConfig
}

// NewCORSHandler creates a CORSHandler from config.
func NewCORSHandler(config CORSConfig) *CORSHandler {
	return &CORSHandler{config: config}
}

// Handle wraps next with CORS headers. Only a real preflight (OPTIONS with both
// Origin and Access-Control-Request-Method) is answered locally; plain OPTIONS
// requests still reach the route, so handlers can implement their own OPTIONS
// semantics.
func (h *CORSHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.writeHeaders(w, r)
		if h.isPreflight(r) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// AppendHeaders returns a middleware that only adds the configured extra allowed
// request headers to preflight responses. Mount it before Handle: Handle merges
// the values into one Access-Control-Allow-Headers header, so the order of the
// two options does not matter.
func (h *CORSHandler) AppendHeaders(next http.HandlerFunc) http.HandlerFunc {
	if len(h.config.Headers) == 0 {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if h.isPreflight(r) {
			header := w.Header()
			header.Set(headerAllowHeaders,
				h.mergeAllowHeaders(header.Get(headerAllowHeaders), "", h.config.Headers))
		}
		next(w, r)
	}
}

// isPreflight reports whether r is a CORS preflight request: an OPTIONS request
// that announces both the requesting origin and the intended method.
func (h *CORSHandler) isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions &&
		r.Header.Get(headerOrigin) != "" &&
		r.Header.Get(headerRequestMethod) != ""
}

func (h *CORSHandler) writeHeaders(w http.ResponseWriter, r *http.Request) {
	h.setVary(w, r)

	header := w.Header()
	if origin := h.allowedOrigin(r.Header.Get(headerOrigin)); origin != "" {
		header.Set(headerAllowOrigin, origin)
		header.Set(headerExposeHeaders, exposeHeaders)
		// Methods, allowed request headers and max-age are only meaningful on a
		// preflight response; keep them off ordinary responses.
		if h.isPreflight(r) {
			header.Set(headerAllowMethods, allowMethods)
			header.Set(headerAllowHeaders,
				h.mergeAllowHeaders(header.Get(headerAllowHeaders), allowHeaders, h.config.Headers))
			header.Set(headerMaxAge, maxAgeSeconds)
		}
		if origin == allowAnyOrigin {
			header.Del(headerAllowCredentials)
		} else {
			header.Set(headerAllowCredentials, allowTrue)
		}
	}
}

// mergeAllowHeaders combines the built-in allow-list, the configured extras and
// any value the response already carries into one comma-separated list, keeping
// the first occurrence of each header name.
func (h *CORSHandler) mergeAllowHeaders(existing, base string, extra []string) string {
	seen := make(map[string]struct{}, 8)
	parts := make([]string, 0, 8)
	add := func(value string) {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			parts = append(parts, part)
		}
	}
	add(base)
	for _, header := range extra {
		add(header)
	}
	add(existing)
	return strings.Join(parts, ", ")
}

func (h *CORSHandler) setVary(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	h.addVary(header, headerOrigin)
	if r.Method == http.MethodOptions {
		h.addVary(header, headerRequestMethod)
		h.addVary(header, headerRequestHeaders)
	}
}

// addVary appends value unless it is already present, so mounting more than one
// CORS handler does not duplicate Vary entries.
func (h *CORSHandler) addVary(header http.Header, value string) {
	for _, existing := range header.Values(headerVary) {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add(headerVary, value)
}

func (h *CORSHandler) allowedOrigin(origin string) string {
	if len(h.config.Origins) == 0 {
		return allowAnyOrigin
	}

	for _, allowed := range h.config.Origins {
		if allowed == allowAnyOrigin {
			return allowAnyOrigin
		}
		if h.matchesOrigin(allowed, origin) {
			return origin
		}
	}
	return ""
}

func (h *CORSHandler) matchesOrigin(allowed, origin string) bool {
	allowed = h.normalizeOrigin(allowed)
	origin = h.normalizeOrigin(origin)
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
func (h *CORSHandler) normalizeOrigin(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "/"))
}
