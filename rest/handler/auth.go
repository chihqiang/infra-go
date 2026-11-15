package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chihqiang/infra-go/rest/token"
)

// AuthorizationHeader is the request header that carries the bearer token.
const AuthorizationHeader = "Authorization"

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

// UnauthorizedCallback handles rejected authentication. It may set custom
// headers and write a body; the middleware still applies the default HTTP status
// when the callback does not set one. When the callback is nil the middleware
// writes the status and, where applicable, an RFC 6750 WWW-Authenticate
// challenge.
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
	if len(r.Header.Values(AuthorizationHeader)) > 1 {
		return "", ErrMalformedAuthorization
	}

	value := strings.TrimSpace(r.Header.Get(AuthorizationHeader))
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
	status := http.StatusUnauthorized
	errorCode, description := "", ""

	switch {
	case errors.Is(err, ErrAuthUnavailable):
		// A missing parser is a server bug; do not disguise it as a 401.
		status = http.StatusInternalServerError
	case errors.Is(err, ErrMalformedAuthorization):
		status = http.StatusBadRequest
		errorCode, description = "invalid_request", "malformed authorization header"
	case errors.Is(err, token.ErrMissingToken):
		// No error attribute: the client did not present bearer credentials.
	case errors.Is(err, token.ErrInvalidToken):
		errorCode, description = "invalid_token", "the access token is invalid or expired"
	}

	if status != http.StatusInternalServerError {
		setBearerChallenge(w, errorCode, description)
	}

	if callback != nil {
		writer := &unauthorizedWriter{ResponseWriter: w, status: status}
		callback(writer, r, err)
		// The callback may set its own status; otherwise apply the default.
		writer.WriteHeader(status)
		return
	}
	w.WriteHeader(status)
}

// unauthorizedWriter applies the default unauthorized status once, even when
// the callback only sets headers or writes a body without WriteHeader.
type unauthorizedWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *unauthorizedWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *unauthorizedWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(w.status)
	}
	return w.ResponseWriter.Write(p)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *unauthorizedWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
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
