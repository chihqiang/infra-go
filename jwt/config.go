package jwt

import "time"

// Algorithm is the JWT signing algorithm type.
type Algorithm string

const (
	// AlgorithmHS256 is HMAC-SHA256.
	AlgorithmHS256 Algorithm = "HS256"
	// AlgorithmHS384 is HMAC-SHA384.
	AlgorithmHS384 Algorithm = "HS384"
	// AlgorithmHS512 is HMAC-SHA512.
	AlgorithmHS512 Algorithm = "HS512"
)

// Token types.
const (
	// TokenTypeAccess is the access token type.
	TokenTypeAccess = "access"
	// TokenTypeRefresh is the refresh token type.
	TokenTypeRefresh = "refresh"
)

// Config is the JWT configuration.
type Config struct {
	// Secret is the HMAC signing key (HS256/HS384/HS512).
	Secret string `json:",optional"`
	// Issuer is the token issuer, for example "my-app".
	Issuer string `json:",optional"`
	// Audience is the intended audience, for example ["web", "app"].
	Audience []string `json:",optional"`
	// AccessTokenExpire is the access token lifetime, 2 hours by default.
	AccessTokenExpire time.Duration `json:",default=2h"`
	// RefreshTokenExpire is the refresh token lifetime, 168 hours (7 days) by default.
	RefreshTokenExpire time.Duration `json:",default=168h"`
	// Algorithm is the signing algorithm, HS256 by default.
	Algorithm Algorithm `json:",default=HS256"`
}

// TokenPair holds an access token and a refresh token.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64 // access token expiry as a Unix timestamp in seconds
}
