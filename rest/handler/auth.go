package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chihqiang/ten-go/rest/internal/pathmatch"
	"github.com/chihqiang/ten-go/rest/token"
)

const (
	// AuthorizationHeader is the request header that carries the bearer token.
	AuthorizationHeader = "Authorization"
	bearerScheme        = "bearer"
)

var (
	// ErrMalformedAuthorization reports a syntactically invalid bearer
	// Authorization header.
	ErrMalformedAuthorization = errors.New("handler: malformed authorization header")
)

// UnauthorizedCallback handles rejected authentication. It may set custom
// headers and write a body; the middleware still applies the default 401 status
// when the callback does not set one. When the callback is nil the middleware
// writes a bodyless 401.
type UnauthorizedCallback func(w http.ResponseWriter, r *http.Request, err error)

// TokenExtractor reads the raw token from a request. It lets the handler accept
// credentials from anywhere, such as a query parameter, a cookie or a custom
// header.
//
// Any non-nil error rejects the request with 401; token.ErrMissingToken is the
// conventional value for "no credentials".
type TokenExtractor func(r *http.Request) (string, error)

// AuthConfig configures AuthHandler. It can be loaded from a config file:
// core/conf and core/mapping fill the exported fields directly.
type AuthConfig struct {
	// Secret signs and verifies bearer tokens.
	Secret string `json:",optional"`
	// PrevSecret also accepts tokens signed with the previous secret while keys
	// rotate.
	PrevSecret string `json:",optional"`
	// SkipPaths lists request paths that bypass authentication. Rules support an
	// exact path, a trailing "*" prefix and glob patterns.
	SkipPaths []string `json:",optional"`
	// Callback optionally customises the rejection response.
	Callback UnauthorizedCallback `json:"-"`
	// TokenExtractor reads the token from the request; it defaults to
	// BearerToken. Set it to accept tokens from a query parameter, a cookie or
	// a custom header. It cannot be loaded from a config file.
	TokenExtractor TokenExtractor `json:"-"`
}

// BearerToken extracts the bearer credentials from r following RFC 6750. It
// returns ErrMalformedAuthorization for a malformed Bearer request and
// token.ErrMissingToken when the request carries no bearer credentials.
func BearerToken(r *http.Request) (string, error) {
	if len(r.Header.Values(AuthorizationHeader)) > 1 {
		return "", ErrMalformedAuthorization
	}

	value := strings.TrimSpace(r.Header.Get(AuthorizationHeader))
	if value == "" {
		return "", token.ErrMissingToken
	}

	// The scheme is a token on its own: "Bearer2 abc" or "Bearerfoo abc" is a
	// non-bearer credential, not a malformed bearer one. Such requests carry no
	// bearer credentials and get a plain 401 (RFC 6750, section 3.1).
	if len(value) < len(bearerScheme) || !strings.EqualFold(value[:len(bearerScheme)], bearerScheme) {
		return "", token.ErrMissingToken
	}
	if len(value) > len(bearerScheme) && value[len(bearerScheme)] != ' ' && value[len(bearerScheme)] != '\t' {
		return "", token.ErrMissingToken
	}
	// "Bearer" without credentials, or with credential characters a b64token
	// cannot contain, is a malformed bearer request.
	if len(value) == len(bearerScheme) {
		return "", ErrMalformedAuthorization
	}

	value = strings.TrimSpace(value[len(bearerScheme):])
	if value == "" || strings.ContainsAny(value, " \t") {
		return "", ErrMalformedAuthorization
	}
	return value, nil
}

// AuthOption customises AuthConfig. Options apply after the struct is filled,
// so they override config-file values and work with AuthConfig literals alike.
type AuthOption func(*AuthConfig)

// WithPrevSecret also accepts tokens signed with secret, for key rotation.
func WithPrevSecret(secret string) AuthOption {
	return func(cfg *AuthConfig) {
		cfg.PrevSecret = secret
	}
}

// WithAuthSkipPaths lists request paths that bypass authentication. Rules
// support an exact path, a trailing "*" prefix and glob patterns.
func WithAuthSkipPaths(paths ...string) AuthOption {
	return func(cfg *AuthConfig) {
		cfg.SkipPaths = append(cfg.SkipPaths, paths...)
	}
}

// WithUnauthorizedCallback customises the rejection response. The middleware
// still applies the default HTTP status when the callback does not set one.
func WithUnauthorizedCallback(callback UnauthorizedCallback) AuthOption {
	return func(cfg *AuthConfig) {
		cfg.Callback = callback
	}
}

// WithTokenExtractor overrides how the token is read from the request. Passing
// nil restores the default BearerToken extractor.
func WithTokenExtractor(extractor TokenExtractor) AuthOption {
	return func(cfg *AuthConfig) {
		cfg.TokenExtractor = extractor
	}
}

// AuthHandler rejects requests without a valid bearer token.
type AuthHandler struct {
	config    AuthConfig
	parser    *token.Parser
	skipPaths *pathmatch.Matcher
}

// NewAuthHandler creates an AuthHandler from config, applying opts last. It
// builds the token parser up front.
func NewAuthHandler(config AuthConfig, opts ...AuthOption) *AuthHandler {
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}

	if config.TokenExtractor == nil {
		config.TokenExtractor = BearerToken
	}
	parser := token.NewParser(token.Config{
		Secret:     config.Secret,
		PrevSecret: config.PrevSecret,
	})
	return &AuthHandler{
		config:    config,
		parser:    parser,
		skipPaths: pathmatch.New(config.SkipPaths),
	}
}

// Handle wraps next with bearer-token authorization and stores verified claims
// in the request context.
func (h *AuthHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.skipPaths.Match(r.URL.Path) {
			next(w, r)
			return
		}

		value, err := h.config.TokenExtractor(r)
		if err != nil {
			h.reject(w, r, err)
			return
		}

		claims, err := h.parser.Parse(value)
		if err != nil {
			h.reject(w, r, err)
			return
		}

		next(w, r.WithContext(token.WithClaims(r.Context(), claims)))
	}
}

func (h *AuthHandler) reject(w http.ResponseWriter, r *http.Request, err error) {
	writer := &unauthorizedWriter{ResponseWriter: w}
	if h.config.Callback != nil {
		h.config.Callback(writer, r, err)
	}
	// The callback may set its own status; otherwise apply the default. The
	// wrapper ignores this call when a status was already written.
	writer.WriteHeader(http.StatusUnauthorized)
}

// unauthorizedWriter applies the default unauthorized status once, even when
// the callback only sets headers or writes a body without WriteHeader.
type unauthorizedWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *unauthorizedWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.wrote {
		return
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *unauthorizedWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusUnauthorized)
	}
	return w.ResponseWriter.Write(p)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *unauthorizedWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
