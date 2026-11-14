package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chihqiang/infra-go/rest/token"
)

// Header is the request header that carries the bearer token.
const Header = "Authorization"

const bearerPrefix = "bearer "

// UnauthorizedCallback handles rejected authentication. When it is nil the
// middleware writes a JSON 401 response.
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
				reject(w, r, token.ErrInvalidToken, options.callback)
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
	value := strings.TrimSpace(r.Header.Get(Header))
	if value == "" {
		return "", token.ErrMissingToken
	}
	if len(value) <= len(bearerPrefix) || !strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return "", token.ErrInvalidToken
	}

	value = strings.TrimSpace(value[len(bearerPrefix):])
	if value == "" {
		return "", token.ErrMissingToken
	}
	return value, nil
}

func reject(w http.ResponseWriter, r *http.Request, err error, callback UnauthorizedCallback) {
	if callback != nil {
		callback(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}{
		Code: http.StatusUnauthorized,
		Msg:  err.Error(),
	})
}
