// Package token issues and verifies JSON Web Tokens for HTTP handlers.
package token

import "errors"

// Errors reported by token verification.
var (
	ErrMissingToken = errors.New("token: token is missing")
	ErrInvalidToken = errors.New("token: token is invalid")
	ErrNoClaims     = errors.New("token: claims are missing")
)

// Config configures token verification. Signing details (expiry and issuer)
// belong to Generate, so they stay out of the configuration.
type Config struct {
	Secret     string `json:",optional"`
	PrevSecret string `json:",optional"`
}
