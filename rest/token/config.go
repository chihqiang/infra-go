// Package token issues and verifies JSON Web Tokens for HTTP handlers.
package token

import (
	"errors"
	"time"
)

const minSecretLength = 8

// Errors reported by configuration validation and token verification.
var (
	ErrShortSecret     = errors.New("token: secret must be at least 8 characters")
	ErrShortPrevSecret = errors.New("token: previous secret must be at least 8 characters")
	ErrMissingToken    = errors.New("token: token is missing")
	ErrInvalidToken    = errors.New("token: token is invalid")
	ErrNoClaims        = errors.New("token: claims are missing")
)

// Config configures token issuing and verification.
type Config struct {
	Secret     string        `json:",optional"`
	PrevSecret string        `json:",optional"`
	Expire     time.Duration `json:",default=24h"`
	Issuer     string        `json:",optional"`
}

// Validate reports configuration problems.
func (c Config) Validate() error {
	if len(c.Secret) < minSecretLength {
		return ErrShortSecret
	}
	// A rotated-out secret is still trusted for verification, so it needs the
	// same minimum length as the current one.
	if c.PrevSecret != "" && len(c.PrevSecret) < minSecretLength {
		return ErrShortPrevSecret
	}
	if c.Expire < 0 {
		return errors.New("token: expire must not be negative")
	}
	return nil
}
