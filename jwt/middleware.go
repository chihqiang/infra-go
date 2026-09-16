package jwt

import (
	"context"
	"errors"
	"net/http"

	"github.com/chihqiang/infra-go/httpx/middleware"
	stdjwt "github.com/golang-jwt/jwt/v5"
)

// Authentication failure response messages.
const (
	msgTokenMissing = "token is missing"
	msgTokenExpired = "token expired"
	msgInvalidToken = "invalid token"
)

// claimCtxKey is the key type used to store a single claim value in a context.
// The unexported type wraps the claim key to avoid clashing with context keys
// from other packages.
type claimCtxKey string

// Claims is an alias for jwt.MapClaims, for convenience.
// MapClaims can be freely extended with arbitrary fields, so no fixed struct
// is needed.
type Claims = stdjwt.MapClaims

// contextKey is the key type used to store claims in a context.
type contextKey struct{}

// claimsKey is the context key under which claims are stored.
var claimsKey = contextKey{}

// WithClaims stores claims in the context and returns the new context.
// They can later be retrieved with ClaimsFromContext.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext extracts claims from the context.
// It returns nil when the context holds no claims.
func ClaimsFromContext(ctx context.Context) Claims {
	claims, _ := ctx.Value(claimsKey).(Claims)
	return claims
}

// bearerChallenge returns the WWW-Authenticate challenge for a 401 response.
//
// RFC 9110 §15.5.2 requires a 401 to carry WWW-Authenticate;
// JWT uses the Bearer scheme, so the challenge is produced per RFC 6750 §3,
// where the error parameter distinguishes "missing credentials" from
// "invalid credentials" so the client can decide whether to redirect to login
// or to refresh the token.
func bearerChallenge(errCode string) middleware.Challenge {
	return middleware.Challenge{Scheme: "Bearer", Error: errCode}
}

// AuthMiddleware returns the JWT authentication middleware.
//
// getToken is supplied by the caller and extracts the token from the request
// (e.g. from a header, cookie or query); the middleware only parses, validates
// and injects the claims and does not care where the token comes from.
//
// On validation failure it returns 401 Unauthorized together with an
// `WWW-Authenticate: Bearer error="..."` challenge as specified by RFC 6750 §3;
// the error response is emitted through the unified error mechanism of
// httpx/middleware (a unified JSON response when the httpx main package is
// imported, otherwise plain text via http.Error).
//
// The return type is func(http.HandlerFunc) http.HandlerFunc, compatible with
// httpx.Middleware, so it can be registered with server.Use directly, or through
// the httpx.WithJWT convenience helper.
func (j *JWT) AuthMiddleware(getToken func(*http.Request) string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := getToken(r)
			if token == "" {
				middleware.WriteUnauthorized(r.Context(), w,
					bearerChallenge(middleware.BearerErrorInvalidRequest), msgTokenMissing)
				return
			}

			claims, err := j.ParseAccessToken(token)
			if err != nil {
				if errors.Is(err, ErrExpiredToken) {
					// An expired token is still an "invalid token" and RFC 6750 has no
					// dedicated value for it; a readable message is kept so the client
					// can tell the cases apart (prompting a refresh, not a re-login).
					middleware.WriteUnauthorized(r.Context(), w,
						bearerChallenge(middleware.BearerErrorInvalidToken), msgTokenExpired)
				} else {
					middleware.WriteUnauthorized(r.Context(), w,
						bearerChallenge(middleware.BearerErrorInvalidToken), msgInvalidToken)
				}
				return
			}

			// Inject each business claim (excluding standard claims and token_type)
			// into the context one by one
			business := extractBusinessClaims(claims)
			ctx := r.Context()
			for k, v := range business {
				ctx = context.WithValue(ctx, claimCtxKey(k), v)
			}
			ctx = WithClaims(ctx, business)
			next(w, r.WithContext(ctx))
		}
	}
}
