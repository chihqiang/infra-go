package token

import (
	"fmt"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const claimSubject = "sub"

// Parser issues and verifies tokens with a single signing secret, optionally
// accepting a previous secret while keys rotate.
type Parser struct {
	conf Config
}

// NewParser validates conf and returns a token parser.
func NewParser(conf Config) (*Parser, error) {
	if err := conf.Validate(); err != nil {
		return nil, err
	}
	return &Parser{conf: conf}, nil
}

// Generate signs claims with the current secret. The caller owns every claim,
// including sub, exp and iss, so the token shape stays explicit at the call
// site. Parse requires sub and exp when verifying.
func (p *Parser) Generate(claims jwtlib.MapClaims) (string, error) {
	if len(claims) == 0 {
		return "", ErrNoClaims
	}

	signed, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).
		SignedString([]byte(p.conf.Secret))
	if err != nil {
		return "", fmt.Errorf("token: sign token: %w", err)
	}
	return signed, nil
}

// Parse verifies token with the current secret, then the previous secret, and
// returns the claims as-is. A token without sub and exp is rejected.
func (p *Parser) Parse(token string) (jwtlib.MapClaims, error) {
	if token == "" {
		return nil, ErrMissingToken
	}

	options := []jwtlib.ParserOption{
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		// Parse requires exp, so tokens without an expiry are rejected even when
		// they were minted elsewhere.
		jwtlib.WithExpirationRequired(),
	}
	secrets := []string{p.conf.Secret}
	if p.conf.PrevSecret != "" {
		secrets = append(secrets, p.conf.PrevSecret)
	}

	var lastErr error
	for _, secret := range secrets {
		parsed, err := jwtlib.Parse(token, func(*jwtlib.Token) (any, error) {
			return []byte(secret), nil
		}, options...)
		if err != nil {
			lastErr = err
			continue
		}

		claims, ok := parsed.Claims.(jwtlib.MapClaims)
		if !ok {
			lastErr = ErrNoClaims
			continue
		}
		if subject, _ := claims[claimSubject].(string); subject == "" {
			lastErr = ErrNoClaims
			continue
		}
		return claims, nil
	}

	return nil, fmt.Errorf("%w: %w", ErrInvalidToken, lastErr)
}
