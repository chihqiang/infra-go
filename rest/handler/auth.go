package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chihqiang/infra-go/rest/token"
)

const (
	// AuthorizationHeader is the request header that carries the bearer token.
	AuthorizationHeader = "Authorization"
	bearerPrefix        = "bearer "
	bearerScheme        = "bearer"
	// authRealm names the default protection space in the WWW-Authenticate challenge.
	authRealm = "api"
)

var (
	// ErrMalformedAuthorization reports a syntactically invalid bearer
	// Authorization header.
	ErrMalformedAuthorization = errors.New("handler: malformed authorization header")
	// ErrAuthUnavailable reports that AuthHandler was constructed without a
	// parser, which is a server configuration error rather than a client failure.
	ErrAuthUnavailable = errors.New("handler: token parser is not configured")
)

// UnauthorizedCallback handles rejected authentication. It may set custom
// headers and write a body; the middleware still applies the default HTTP status
// when the callback does not set one. When the callback is nil the middleware
// writes the status and, where applicable, an RFC 6750 WWW-Authenticate
// challenge.
type UnauthorizedCallback func(w http.ResponseWriter, r *http.Request, err error)

// AuthConfig configures AuthHandler.
type AuthConfig struct {
	Enabled bool `json:",optional"`
	// Parser verifies bearer tokens; it is required when Enabled is true.
	Parser *token.Parser `json:"-"`
	// Realm is the WWW-Authenticate realm; it defaults to "api".
	Realm string `json:",optional"`
	// Callback optionally customises the rejection response.
	Callback UnauthorizedCallback `json:"-"`
}

// AuthHandler rejects requests without a valid bearer token.
type AuthHandler struct {
	config AuthConfig
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(config AuthConfig) *AuthHandler {
	return &AuthHandler{config: config}
}

// Handle wraps next with bearer-token authorization and stores verified claims
// in the request context.
func (h *AuthHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if h.config.Parser == nil {
			h.reject(w, r, ErrAuthUnavailable)
			return
		}

		value, err := bearerToken(r)
		if err != nil {
			h.reject(w, r, err)
			return
		}

		claims, err := h.config.Parser.Parse(value)
		if err != nil {
			h.reject(w, r, err)
			return
		}

		next(w, r.WithContext(token.WithClaims(r.Context(), claims)))
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

func (h *AuthHandler) reject(w http.ResponseWriter, r *http.Request, err error) {
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
		setBearerChallenge(w, h.realm(), errorCode, description)
	}

	if h.config.Callback != nil {
		writer := &unauthorizedWriter{ResponseWriter: w, status: status}
		h.config.Callback(writer, r, err)
		// The callback may set its own status; otherwise apply the default.
		writer.WriteHeader(status)
		return
	}
	w.WriteHeader(status)
}

func (h *AuthHandler) realm() string {
	if h.config.Realm != "" {
		return h.config.Realm
	}
	return authRealm
}

// setBearerChallenge writes the RFC 6750 challenge. code and description are
// fixed literals here, so no quoted-string escaping is required.
func setBearerChallenge(w http.ResponseWriter, realm, code, description string) {
	challenge := `Bearer realm="` + realm + `"`
	if code != "" {
		challenge += `, error="` + code + `"`
	}
	if description != "" {
		challenge += `, error_description="` + description + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
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
