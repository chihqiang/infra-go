package middleware

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Standard response header names related to status code semantics.
const (
	// HeaderWWWAuthenticate is the authentication challenge header a 401 response
	// must carry (RFC 9110 §11.3).
	HeaderWWWAuthenticate = "WWW-Authenticate"
	// HeaderRetryAfter is the retry hint header recommended on 429/503
	// (RFC 9110 §10.2.3).
	HeaderRetryAfter = "Retry-After"
)

// Bearer challenge error values defined by RFC 6750 §3.
const (
	// BearerErrorInvalidRequest means the request is missing required credentials.
	BearerErrorInvalidRequest = "invalid_request"
	// BearerErrorInvalidToken means the token is invalid, expired or revoked.
	BearerErrorInvalidToken = "invalid_token"
	// BearerErrorInsufficientScope means the token is valid but lacks permission.
	BearerErrorInsufficientScope = "insufficient_scope"
)

// Challenge describes the authentication scheme advertised to clients in a 401
// response (RFC 9110 §11.3.1).
//
// Its form is `scheme 1*SP #auth-param`, for example:
//
//	Bearer realm="api", error="invalid_token"
type Challenge struct {
	// Scheme is the authentication scheme name, such as "Bearer" (RFC 6750).
	Scheme string
	// Realm is the protection space identifier; optional.
	Realm string
	// Error is the failure reason, with values from RFC 6750 §3; it is only
	// defined for the Bearer scheme and stays empty for others.
	Error string
}

// String renders the value of the WWW-Authenticate header.
// It returns an empty string when Scheme is empty, letting callers skip setting
// the header.
// auth-param values use quoted-string per RFC 9110 §11.2.
func (c Challenge) String() string {
	scheme := strings.TrimSpace(c.Scheme)
	if scheme == "" {
		return ""
	}

	params := make([]string, 0, 2)
	if c.Realm != "" {
		params = append(params, `realm="`+quoteHeaderValue(c.Realm)+`"`)
	}
	if c.Error != "" {
		params = append(params, `error="`+quoteHeaderValue(c.Error)+`"`)
	}
	if len(params) == 0 {
		return scheme
	}
	return scheme + " " + strings.Join(params, ", ")
}

// quoteHeaderValue escapes characters that may not appear literally inside a
// quoted-string.
// Only visible ASCII and the space are kept — everything else is dropped — to
// avoid header injection (CR/LF) and illegal values.
func quoteHeaderValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r == '"' || r == '\\':
			// Quotes and backslashes inside a quoted-string need backslash escaping
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// Control characters (including CR/LF) are dropped outright to prevent
			// response header injection
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// WriteUnauthorized writes a 401 response, attaching the WWW-Authenticate header
// when one can be provided.
//
// RFC 9110 §15.5.2 states that a server generating a 401 response **MUST** send a
// WWW-Authenticate header containing at least one challenge applicable to the
// target resource.
// Without that header, spec-compliant clients and gateways cannot tell which
// authentication scheme to use, so authentication middleware should emit 401
// responses through this function.
func WriteUnauthorized(ctx context.Context, w http.ResponseWriter, c Challenge, msg string) {
	if v := c.String(); v != "" {
		w.Header().Set(HeaderWWWAuthenticate, v)
	}
	writeError(ctx, w, http.StatusUnauthorized, msg)
}

// WriteRetryAfter writes a 429 / 503 response, attaching a Retry-After header
// whenever a suggestion can be given.
//
// RFC 9110 §10.2.3 allows Retry-After to be either delay-seconds (a non-negative
// decimal integer) or an HTTP-date; delay-seconds is used uniformly here because
// it is simpler and unaffected by clock skew between the two sides.
//
// RFC 6585 §4 uses MAY for 429 and RFC 9110 §15.6.4 uses SHOULD for 503; both
// rely on this header to guide client backoff. When d <= 0 the header is not
// sent: better to omit it than to report a fabricated wait (clients might
// otherwise refrain from retrying for a long time).
func WriteRetryAfter(ctx context.Context, w http.ResponseWriter, status int, d time.Duration, msg string) {
	if secs := RetryAfterSeconds(d); secs > 0 {
		w.Header().Set(HeaderRetryAfter, strconv.Itoa(secs))
	}
	writeError(ctx, w, status, msg)
}

// RetryAfterSeconds rounds the suggested wait duration to the seconds required by
// Retry-After.
//
// It rounds up: better to make the client wait a little longer than to retry
// before the rate limit window / breaker cooldown has ended.
// Anything under one second still returns 1, avoiding "Retry-After: 0" (which
// would be read as "retry immediately").
// d <= 0 returns 0, meaning the header should not be sent.
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		return 1
	}
	return secs
}

// RetryAfterProvider is optionally implemented by limiters that can suggest a
// retry interval.
//
// The rate limit middleware type-asserts this interface when rejecting a request
// in order to derive an accurate Retry-After: a token bucket, for example, can
// compute how long until the next token becomes available, and a sliding window
// can compute when the earliest request slides out of the window.
//
// Implementations that cannot produce a meaningful estimate (such as a
// concurrency limiter) need not implement this interface; the middleware then
// falls back to the RetryAfter option and omits the header when that is 0 too.
type RetryAfterProvider interface {
	RetryAfter() time.Duration
}
