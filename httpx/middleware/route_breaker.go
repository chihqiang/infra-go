package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// RouteBreaker is a circuit breaker middleware isolated per route.
// Each route (METHOD:pattern) owns a separate breaker and their statistics do not
// interfere with each other, so failures of a single route cannot drag down the
// pass rate of the others.
type RouteBreaker struct {
	// retryAfter is the retry interval advertised to clients while the circuit
	// is open; it defaults to breakerRetryAfter.
	retryAfter time.Duration
}

// NewRouteBreaker creates the per-route circuit breaker middleware.
func NewRouteBreaker() *RouteBreaker {
	return &RouteBreaker{retryAfter: breakerRetryAfter}
}

// WithRetryAfter sets the Retry-After hint interval used while the circuit is
// open (RFC 9110 §10.2.3).
// d <= 0 means the header is not sent.
func (b *RouteBreaker) WithRetryAfter(d time.Duration) *RouteBreaker {
	b.retryAfter = d
	return b
}

// breakerName returns the breaker name for this request.
//
// **The route template must be used instead of the concrete path**: breakers are
// cached forever by breaker.GetBreaker, so using r.URL.Path (/users/1, /users/2
// …) would create a new breaker for every distinct path parameter:
//
//   - "per-route isolation" degrades into "per-request isolation" and the breaker
//     statistics become meaningless;
//   - memory grows without bound (an attacker crafting many distinct paths can
//     exhaust it).
//
// Resolution order:
//  1. r.Pattern: filled in by net/http when it dispatches the request to the
//     matched handler (Go 1.23+). Middleware registered at route level (inside the
//     mux) gets it directly.
//  2. The template in the context: global middleware sits outside the mux, where
//     r.Pattern is still empty, so httpx.Server pre-matches the route and writes
//     it into the context (see PatternFromContext).
//  3. The normalized path: for other frameworks / scenarios without
//     pre-matching, identifier-like segments are replaced with a placeholder so
//     that the name cardinality stays bounded.
func breakerName(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	if pattern := PatternFromContext(r.Context()); pattern != "" {
		return pattern
	}
	return r.Method + ":" + normalizePathPattern(r.URL.Path)
}

// pathIDPlaceholder is the placeholder for "parameter segments" in a normalized
// path.
const pathIDPlaceholder = "{}"

// maxPatternSegments is the maximum number of segments kept when normalizing a
// path, so that a very long path cannot inflate the name length.
const maxPatternSegments = 16

// normalizePathPattern normalizes a concrete path into template form so that the
// cardinality stays bounded.
//
// A segment counts as a "parameter segment" when it is:
//   - all digits (/users/123)
//   - long hexadecimal (/files/a1b2c3d4…, common for hashes or dash-less UUIDs)
//   - UUID-shaped with a dash and length >= 32
//   - any other long string of length >= 16 (treated conservatively as an id)
//
// Example: /users/123/orders/456 → /users/{}/orders/{}
func normalizePathPattern(p string) string {
	if p == "" {
		return "/"
	}

	segments := strings.Split(p, "/")
	if len(segments) > maxPatternSegments {
		segments = segments[:maxPatternSegments]
	}

	for i, seg := range segments {
		if isIdentifierSegment(seg) {
			segments[i] = pathIDPlaceholder
		}
	}
	return strings.Join(segments, "/")
}

// isIdentifierSegment reports whether a path segment looks like an "identifier"
// (rather than a fixed route segment).
func isIdentifierSegment(seg string) bool {
	switch len(seg) {
	case 0:
		// An empty segment comes from a leading/trailing "/" or a double slash;
		// keep it as is.
		return false
	case 1:
		// A single digit counts as an identifier (/users/0); a single letter is a
		// fixed route segment (/v/a etc.) and is kept.
		return seg[0] >= '0' && seg[0] <= '9'
	}

	allDigits := true
	allHex := true
	for _, c := range seg {
		switch {
		case c >= '0' && c <= '9':
			// A digit satisfies both predicates
		case (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
			allDigits = false
		case c == '-' || c == '_':
			// A separator: no longer all digits, and no longer all hex
			allDigits = false
			allHex = false
		default:
			allDigits = false
			allHex = false
		}
	}

	return allDigits || (allHex && len(seg) >= 8) || len(seg) >= 16
}

// Middleware returns the per-route circuit breaker middleware in the standard
// form func(http.Handler) http.Handler.
// Breakers are cached by name through breaker.GetBreaker, so one route template
// shares a single instance.
// While the circuit is open it returns 503 Service Unavailable (with a
// Retry-After); a successful request (<500) reports Accept and a failed request
// (>=500) reports Reject.
func (b *RouteBreaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := breakerName(r)
			brk := breaker.GetBreaker(name)

			promise, err := brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by route breaker",
					logger.String("breaker", name),
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
					logger.Err(err),
				)
				// RFC 9110 §15.6.4: a 503 returned because of a failure/overload SHOULD
				// carry Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					b.retryAfter, "service unavailable")
				return
			}

			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			if rec.Status() < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.Status(), http.StatusText(rec.Status())))
			}
		})
	}
}
