// Package cors implements Cross-Origin Resource Sharing for the HTTP server.
package cors

import (
	"net/http"
	"strings"

	"github.com/chihqiang/infra-go/rest/internal/response"
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
func Middleware(headerFn func(http.Header), origins ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			writeHeaders(w, r, origins)
			if headerFn != nil {
				headerFn(w.Header())
			}

			if r.Method == http.MethodOptions {
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
		if r.Method == http.MethodOptions {
			status = http.StatusNoContent
		}
		writer.Flush(status)
	})
}

func writeHeaders(w http.ResponseWriter, r *http.Request, origins []string) {
	setVary(w, r)

	origin := allowedOrigin(origins, r.Header.Get(headerOrigin))
	if origin == "" {
		return
	}

	header := w.Header()
	header.Set(headerAllowOrigin, origin)
	header.Set(headerAllowMethods, allowMethods)
	header.Set(headerAllowHeaders, allowHeaders)
	header.Set(headerExposeHeaders, exposeHeaders)
	header.Set(headerMaxAge, maxAgeSeconds)
	if origin == allowAnyOrigin {
		header.Del(headerAllowCredentials)
	} else {
		header.Set(headerAllowCredentials, allowTrue)
	}
}

func setVary(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Add(headerVary, headerOrigin)
	if r.Method == http.MethodOptions {
		header.Add(headerVary, headerRequestMethod)
		header.Add(headerVary, headerRequestHeaders)
	}
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
	allowed = strings.ToLower(strings.TrimSuffix(allowed, "/"))
	origin = strings.ToLower(strings.TrimSuffix(origin, "/"))
	if allowed == "" || origin == "" {
		return false
	}
	return origin == allowed || strings.HasSuffix(origin, "."+allowed)
}
