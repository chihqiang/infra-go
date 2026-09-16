package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/chihqiang/infra-go/mapping"
	stdjwt "github.com/golang-jwt/jwt/v5"
)

// Error definitions.
var (
	// ErrInvalidToken indicates an invalid token.
	ErrInvalidToken = errors.New("jwt: invalid token")
	// ErrExpiredToken indicates an expired token.
	ErrExpiredToken = errors.New("jwt: token is expired")
	// ErrNotRefreshToken indicates the token is not a refresh token.
	ErrNotRefreshToken = errors.New("jwt: token is not a refresh token")
	// ErrSecretEmpty indicates an empty secret.
	ErrSecretEmpty = errors.New("jwt: secret is empty")
	// ErrUnsupportedAlgorithm indicates an unsupported signing algorithm.
	ErrUnsupportedAlgorithm = errors.New("jwt: unsupported algorithm")
)

// fillDefault fills in the defaults and then overrides them with the non-zero
// fields from the user configuration.
// Delegated to mapping.FillAndOverride.
func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	return c
}

// signingMethod returns the jwt.SigningMethod matching the algorithm.
func signingMethod(alg Algorithm) (stdjwt.SigningMethod, error) {
	switch alg {
	case AlgorithmHS256:
		return stdjwt.SigningMethodHS256, nil
	case AlgorithmHS384:
		return stdjwt.SigningMethodHS384, nil
	case AlgorithmHS512:
		return stdjwt.SigningMethodHS512, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, alg)
	}
}

// --- JWT object-oriented wrapper ---

// JWT wraps JWT operations; the configuration only needs to be initialized once.
type JWT struct {
	config Config
	method stdjwt.SigningMethod
}

// New creates a JWT instance; the configuration is only passed in once.
func New(cfg Config) (*JWT, error) {
	c := fillDefault(cfg)

	if c.Secret == "" {
		return nil, ErrSecretEmpty
	}

	method, err := signingMethod(c.Algorithm)
	if err != nil {
		return nil, err
	}

	return &JWT{
		config: c,
		method: method,
	}, nil
}

// MustNew creates a JWT instance and panics on error.
// Suitable for global initialization scenarios.
func MustNew(cfg Config) *JWT {
	j, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return j
}

// Config returns the configuration with defaults filled in.
func (j *JWT) Config() Config {
	return j.config
}

// --- Token generation ---

// GenerateToken generates a single token.
// claims holds the custom claims; the standard iss, aud, iat, nbf and exp
// fields are injected automatically. If "token_type" is not set in claims,
// the caller must set it. expire is the token's time to live.
func (j *JWT) GenerateToken(claims Claims, expire time.Duration) (string, error) {
	// Copy the claims so the map passed in by the caller is not modified.
	claims = copyClaims(claims)

	now := time.Now()

	// Inject the standard claims
	claims[ClaimKeyIssuedAt] = now.Unix()
	claims[ClaimKeyNotBefore] = now.Unix()
	claims[ClaimKeyExpirationTime] = now.Add(expire).Unix()
	if j.config.Issuer != "" {
		claims[ClaimKeyIssuer] = j.config.Issuer
	}
	if len(j.config.Audience) > 0 {
		claims[ClaimKeyAudience] = j.config.Audience
	}

	token := stdjwt.NewWithClaims(j.method, claims)
	return token.SignedString([]byte(j.config.Secret))
}

// GenerateAccessToken generates an access token, setting token_type to access.
func (j *JWT) GenerateAccessToken(claims Claims) (string, error) {
	claims = copyClaims(claims)
	claims[ClaimKeyTokenType] = TokenTypeAccess
	return j.GenerateToken(claims, j.config.AccessTokenExpire)
}

// GenerateRefreshToken generates a refresh token, setting token_type to refresh.
func (j *JWT) GenerateRefreshToken(claims Claims) (string, error) {
	claims = copyClaims(claims)
	claims[ClaimKeyTokenType] = TokenTypeRefresh
	return j.GenerateToken(claims, j.config.RefreshTokenExpire)
}

// GenerateTokenPair generates an access token and refresh token pair.
// Custom fields in claims are written into both tokens.
func (j *JWT) GenerateTokenPair(claims Claims) (*TokenPair, error) {
	// Copy the claims so the two tokens do not share the same backing data
	accessClaims := copyClaims(claims)
	refreshClaims := copyClaims(claims)

	accessToken, err := j.GenerateAccessToken(accessClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := j.GenerateRefreshToken(refreshClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(j.config.AccessTokenExpire).Unix(),
	}, nil
}

// --- Token validation ---

// parserOptions builds the parse-time validation options from the configuration.
//
// Key point: iss/aud must be validated here, not merely written at signing time.
// The old implementation only wrote Issuer/Audience into the token and never
// validated them, so different applications sharing the same secret
// (or multi-tenant setups) accepted each other's tokens — for example a token
// with iss=app-a/aud=tenant-a was accepted by an instance configured with
// app-b/tenant-b (verified empirically), allowing cross-tenant privilege
// escalation.
func (j *JWT) parserOptions() []stdjwt.ParserOption {
	opts := []stdjwt.ParserOption{
		// Restrict the allowed signing algorithm as a second line of defence
		// against algorithm-confusion attacks (keyfunc also verifies
		// token.Method.Alg()).
		stdjwt.WithValidMethods([]string{string(j.config.Algorithm)}),
	}
	if j.config.Issuer != "" {
		opts = append(opts, stdjwt.WithIssuer(j.config.Issuer))
	}
	if len(j.config.Audience) > 0 {
		// WithAudience: the token's aud must contain at least one configured value
		opts = append(opts, stdjwt.WithAudience(j.config.Audience...))
	}
	return opts
}

// ParseToken parses and validates a token, returning its claims.
//
// Validation covers: the signature and algorithm, exp/nbf, and the iss/aud
// claims **only when configured**. If Config leaves Issuer/Audience unset, the
// corresponding claim is not validated (for compatibility with the existing
// "secret-only validation" usage).
func (j *JWT) ParseToken(tokenString string) (Claims, error) {
	claims := Claims{}

	token, err := stdjwt.ParseWithClaims(tokenString, claims, func(token *stdjwt.Token) (any, error) {
		// Strictly verify that the signing algorithm matches the configuration,
		// preventing algorithm-confusion attacks. Compare the Alg() string
		// rather than the method pointer: method pointers rely on golang-jwt's
		// internal singletons, so a pointer comparison breaks if someone
		// registers a custom implementation via RegisterSigningMethod
		// (or a future SDK returns a new instance).
		if alg := token.Method.Alg(); alg != string(j.config.Algorithm) {
			return nil, fmt.Errorf("%w: unexpected signing method: %v", ErrInvalidToken, alg)
		}
		return []byte(j.config.Secret), nil
	}, j.parserOptions()...)

	if err != nil {
		if errors.Is(err, stdjwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err.Error())
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ParseAccessToken parses an access token and verifies its type is access.
func (j *JWT) ParseAccessToken(tokenString string) (Claims, error) {
	claims, err := j.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}

	if tt, _ := claims[ClaimKeyTokenType].(string); tt != TokenTypeAccess {
		return nil, fmt.Errorf("%w: expected access token, got %s", ErrInvalidToken, tt)
	}

	return claims, nil
}

// ParseRefreshToken parses a refresh token and verifies its type is refresh.
func (j *JWT) ParseRefreshToken(tokenString string) (Claims, error) {
	claims, err := j.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}

	if tt, _ := claims[ClaimKeyTokenType].(string); tt != TokenTypeRefresh {
		return nil, ErrNotRefreshToken
	}

	return claims, nil
}

// --- Token refresh ---

// RefreshToken generates a new token pair from a refresh token.
// Once the old refresh token passes validation, its custom claims are extracted
// and a brand new token pair is generated.
func (j *JWT) RefreshToken(refreshToken string) (*TokenPair, error) {
	claims, err := j.ParseRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Drop the standard claims and token_type, keeping only business fields
	cleanClaims := extractBusinessClaims(claims)

	return j.GenerateTokenPair(cleanClaims)
}

// --- Internal helpers ---

// copyClaims deep-copies MapClaims to avoid sharing the backing data.
func copyClaims(src Claims) Claims {
	dst := make(Claims, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// standardClaimKeys is the set of standard JWT claim keys plus token_type.
// Declared as a package-level variable so extractBusinessClaims does not rebuild
// it on every call.
var standardClaimKeys = map[string]bool{
	ClaimKeyIssuer:         true,
	ClaimKeyAudience:       true,
	ClaimKeySubject:        true,
	ClaimKeyExpirationTime: true,
	ClaimKeyIssuedAt:       true,
	ClaimKeyNotBefore:      true,
	ClaimKeyTokenType:      true,
	ClaimKeyJWTID:          true,
}

// extractBusinessClaims extracts the business fields from claims,
// removing the standard claims and token_type.
func extractBusinessClaims(claims Claims) Claims {
	result := make(Claims)
	for k, v := range claims {
		if !standardClaimKeys[k] {
			result[k] = v
		}
	}
	return result
}
