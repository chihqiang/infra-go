package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chihqiang/infra-go/rest/token"
)

// Header is the request header that carries the bearer token.
const Header = "Authorization"

const (
	bearerPrefix = "bearer "
	bearerScheme = "bearer"
	// authRealm names the protection space in the WWW-Authenticate challenge.
	authRealm = "api"
)

var (
	// ErrMalformedAuthorization reports a syntactically invalid bearer
	// Authorization header.
	ErrMalformedAuthorization = errors.New("handler: malformed authorization header")
	// ErrAuthUnavailable reports that Authorize was constructed without a parser,
	// which is a server configuration error rather than a client failure.
	ErrAuthUnavailable = errors.New("handler: token parser is not configured")
)

// UnauthorizedCallback handles rejected authentication. When it is nil the
// middleware writes a JSON response and, where applicable, an RFC 6750
// WWW-Authenticate challenge.
type UnauthorizedCallback func(w http.ResponseWriter, r *http.Request, err error)

// AuthorizeOption customizes Authorize.
type AuthorizeOption func(*authorizeOptions)

type authorizeOptions struct {
	callback UnauthorizedCallback
}

// WithUnauthorizedCallback installs a custom rejection handler.
func WithUnauthorizedCallback(callback UnauthorizedCallback) AuthorizeOption {
	return func(opts *authorizeOptions) {
		opts.callback = callback
	}
}

// Authorize rejects requests without a valid bearer token and stores the
// verified claims in the request context.
func Authorize(parser *token.Parser, opts ...AuthorizeOption) func(http.HandlerFunc) http.HandlerFunc {
	var options authorizeOptions
	for _, opt := range opts {
		opt(&options)
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if parser == nil {
				reject(w, r, ErrAuthUnavailable, options.callback)
				return
			}

			value, err := bearerToken(r)
			if err != nil {
				reject(w, r, err, options.callback)
				return
			}

			claims, err := parser.Parse(value)
			if err != nil {
				reject(w, r, err, options.callback)
				return
			}

			next(w, r.WithContext(token.WithClaims(r.Context(), claims)))
		}
	}
}

func bearerToken(r *http.Request) (string, error) {
	if len(r.Header.Values(Header)) > 1 {
		return "", ErrMalformedAuthorization
	}

	value := strings.TrimSpace(r.Header.Get(Header))
	if value == "" {
		return "", token.ErrMissingToken
	}

	// An unsupported scheme means the request carries no bearer credentials:
	// answer 401 with a bare challenge (RFC 6750, section 3.1).
	if len(value) < len(bearerScheme) || !strings.EqualFold(value[:len(bearerScheme)], bearerScheme) {
		return "", token.ErrMissingToken
	}
	// "Bearer" without the separating space, or with empty credentials, is a
	// malformed bearer request.
	if len(value) <= len(bearerPrefix) || !strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return "", ErrMalformedAuthorization
	}

	value = strings.TrimSpace(value[len(bearerPrefix):])
	if value == "" || strings.ContainsAny(value, " \t") {
		return "", ErrMalformedAuthorization
	}
	return value, nil
}

func reject(w http.ResponseWriter, r *http.Request, err error, callback UnauthorizedCallback) {
	if callback != nil {
		callback(w, r, err)
		return
	}

	switch {
	case errors.Is(err, ErrAuthUnavailable):
		// A missing parser is a server bug; do not disguise it as a 401.
		w.WriteHeader(http.StatusInternalServerError)
	case errors.Is(err, ErrMalformedAuthorization):
		setBearerChallenge(w, "invalid_request", "malformed authorization header")
		w.WriteHeader(http.StatusBadRequest)
	case errors.Is(err, token.ErrMissingToken):
		// No error attribute: the client did not present bearer credentials.
		setBearerChallenge(w, "", "")
		w.WriteHeader(http.StatusUnauthorized)
	case errors.Is(err, token.ErrInvalidToken):
		setBearerChallenge(w, "invalid_token", "the access token is invalid or expired")
		w.WriteHeader(http.StatusUnauthorized)
	default:
		setBearerChallenge(w, "", "")
		w.WriteHeader(http.StatusUnauthorized)
	}
}

// setBearerChallenge writes the RFC 6750 challenge. code and description are
// fixed literals here, so no quoted-string escaping is required.
func setBearerChallenge(w http.ResponseWriter, code, description string) {
	challenge := `Bearer realm="` + authRealm + `"`
	if code != "" {
		challenge += `, error="` + code + `"`
	}
	if description != "" {
		challenge += `, error_description="` + description + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
}
