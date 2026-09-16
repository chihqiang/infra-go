package middleware

import "net/http"

// CORS response header constants (used only inside the CORS logic, inlined in this file).
const (
	corsAllowAll        = "*"
	corsHeaderOrigin    = "Origin"
	corsHeaderVary      = "Vary"
	corsAllowOrigin     = "Access-Control-Allow-Origin"
	corsAllowMethods    = "Access-Control-Allow-Methods"
	corsAllowHeaders    = "Access-Control-Allow-Headers"
	corsExposeHeaders   = "Access-Control-Expose-Headers"
	corsAllowCredential = "Access-Control-Allow-Credentials"
	corsMaxAge          = "Access-Control-Max-Age"
	corsDefaultMethods  = "GET, POST, PUT, DELETE, OPTIONS, PATCH"
	corsDefaultHeaders  = "Content-Type, Authorization, X-Requested-With, Accept, Origin"
	corsDefaultExpose   = "Content-Length, Content-Type"
	corsDefaultCreds    = "true"
	corsDefaultMaxAge   = "86400"
	schemeHTTP          = "http"
	schemeHTTPS         = "https"
)

// CORS is a middleware that sets CORS headers on responses.
// The set of allowed origins is prebuilt at construction time, avoiding an O(n) scan per request.
type CORS struct {
	allowAll bool
	allowed  map[string]struct{}
	// credentials controls whether Access-Control-Allow-Credentials: true is sent; default true.
	credentials bool
	// rejectUnauthorized controls whether an unauthorized origin is rejected with 403;
	// default false (pass through).
	rejectUnauthorized bool
}

// NewCORS creates the CORS middleware.
// allowOrigins is the list of allowed origins; passing "*" allows every origin (it takes
// precedence over the other entries).
//
// About credentials (Access-Control-Allow-Credentials):
// Per the Fetch specification, "allow all origins" and "allow credentials" cannot both be
// expressed with a wildcard — browsers reject the combination of
// `Access-Control-Allow-Origin: *` and `Access-Control-Allow-Credentials: true`. Therefore in
// allowAll mode this middleware echoes the concrete Origin (along with Vary: Origin) instead of
// sending "*".
//
// ⚠️ Security note: allowAll + credentials means **any site** can issue credentialed
// cross-origin requests and read the responses. In production, switch to an explicit origin
// list, or turn credentials off with WithCredentials(false) when cookie/Authorization are
// definitely not needed.
//
// An unauthorized origin is by default **passed through** (no CORS headers are sent and the
// request continues to the downstream handler); see WithRejectUnauthorizedOrigin.
func NewCORS(allowOrigins ...string) *CORS {
	c := &CORS{
		allowed:     make(map[string]struct{}, len(allowOrigins)),
		credentials: true,
	}
	for _, o := range allowOrigins {
		if o == corsAllowAll {
			c.allowAll = true
			break
		}
		c.allowed[o] = struct{}{}
	}
	return c
}

// WithCredentials sets whether Access-Control-Allow-Credentials is sent.
//
// It defaults to true (preserving the existing behaviour). When set to false the response header
// is not sent, browsers will forbid cross-origin requests from carrying cookies / TLS client
// certificates, and the Authorization header is excluded from what can be exposed — suitable for
// purely public APIs.
func (c *CORS) WithCredentials(enable bool) *CORS {
	c.credentials = enable
	return c
}

// WithRejectUnauthorizedOrigin sets whether an unauthorized origin is directly rejected with
// 403; default false.
//
// **Default pass-through (false)**: no CORS headers are sent and the request continues to the
// downstream handler. This is the standard CORS approach, because:
//
//   - CORS is a **browser-side** response reading restriction, not server-side request access
//     control. Without Access-Control-Allow-Origin the browser already stops scripts from reading
//     the response, so the server does not need to (and should not) reject it a second time.
//   - Returning 403 also hits **non-browser clients**: curl, mobile apps, service-to-service
//     calls and some HTTP libraries always send an Origin header. They are not bound by CORS,
//     yet a 403 would keep them out of the business logic.
//   - A preflight (OPTIONS) can never succeed anyway, so the browser never sends the real
//     request; and if a real request is let through, its response still cannot be read by
//     cross-origin scripts, so passing through costs nothing in security terms.
//
// Setting it to true restores the strict "unauthorized origin → 403" behaviour, whose meaning is
// "only serve the listed origins" rather than "browsers cannot read". Choose according to your
// actual needs: if the service really only targets whitelisted origins (for example the backend
// of a pure front-end app), strict mode can reject early and save pointless downstream work; but
// do **not** treat CORS as CSRF protection — simple requests (form POST, img GET) are not blocked
// by CORS in the first place, so state-changing endpoints should separately use CSRF tokens or
// SameSite cookies.
func (c *CORS) WithRejectUnauthorizedOrigin(reject bool) *CORS {
	c.rejectUnauthorized = reject
	return c
}

// Middleware returns the CORS middleware in the standard func(http.Handler) http.Handler form.
//
// Same-origin requests (Origin matches Host) get no CORS headers; allowed origins get CORS
// headers with OPTIONS preflight answered by 204; unauthorized origins pass through by default
// (no CORS headers are sent), which WithRejectUnauthorizedOrigin(true) changes into a 403.
func (c *CORS) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(corsHeaderOrigin)
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Same-origin requests need no CORS headers
			scheme := schemeHTTP
			if r.TLS != nil {
				scheme = schemeHTTPS
			}
			if origin == scheme+"://"+r.Host {
				next.ServeHTTP(w, r)
				return
			}

			// Validate the Origin
			if !c.allowAll {
				if _, ok := c.allowed[origin]; !ok {
					if c.rejectUnauthorized {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					// Pass through: no CORS headers are sent, so the browser blocks scripts from reading
					// the response. Non-browser clients are unaffected.
					next.ServeHTTP(w, r)
					return
				}
			}

			// Echo the concrete Origin: the response varies with Origin, so Vary must be declared to
			// keep caches from serving it to the wrong client.
			//
			// allowAll mode does **not** send "*" any more: combined with Allow-Credentials: true
			// the browser rejects the whole response, which made credentialed cross-origin requests
			// (withCredentials / credentials: 'include') fail unconditionally in the old
			// implementation. Echoing the Origin satisfies both "allow any origin" and "allow
			// credentials", and is the only form browsers accept.
			w.Header().Set(corsAllowOrigin, origin)
			w.Header().Set(corsHeaderVary, corsHeaderOrigin)
			w.Header().Set(corsAllowMethods, corsDefaultMethods)
			w.Header().Set(corsAllowHeaders, corsDefaultHeaders)
			w.Header().Set(corsExposeHeaders, corsDefaultExpose)
			if c.credentials {
				w.Header().Set(corsAllowCredential, corsDefaultCreds)
			}
			w.Header().Set(corsMaxAge, corsDefaultMaxAge)

			// OPTIONS preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
