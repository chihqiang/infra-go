package token

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	claimSubject = "sub"
	claimIssued  = "iat"
	claimExpires = "exp"
	claimIssuer  = "iss"
)

// Claims is the verified token payload.
type Claims struct {
	Subject   string
	Issuer    string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Data      map[string]any
}

// Parser issues and verifies tokens with a single signing secret, optionally
// accepting a previous secret while keys rotate.
type Parser struct {
	conf Config
}

// NewParser validates conf and returns a token parser.
func NewParser(conf Config) (*Parser, error) {
	if conf.Expire == 0 {
		conf.Expire = 24 * time.Hour
	}
	if err := conf.Validate(); err != nil {
		return nil, err
	}
	return &Parser{conf: conf}, nil
}

// Config returns a copy of the manager configuration.
func (p *Parser) Config() Config {
	return p.conf
}

// Generate signs a token for subject, adding data as custom claims.
func (p *Parser) Generate(subject string, data map[string]any) (string, error) {
	if subject == "" {
		return "", errors.New("token: subject must not be empty")
	}

	now := time.Now()
	claims := jwtlib.MapClaims{
		claimSubject: subject,
		claimIssued:  now.Unix(),
	}
	if p.conf.Expire != 0 {
		claims[claimExpires] = now.Add(p.conf.Expire).Unix()
	}
	if p.conf.Issuer != "" {
		claims[claimIssuer] = p.conf.Issuer
	}
	for key, value := range data {
		switch key {
		case claimSubject, claimIssued, claimExpires, claimIssuer:
			continue
		}
		claims[key] = value
	}

	signed, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString([]byte(p.conf.Secret))
	if err != nil {
		return "", fmt.Errorf("token: sign token: %w", err)
	}
	return signed, nil
}

// Parse verifies token with the current secret, then the previous secret.
func (p *Parser) Parse(token string) (*Claims, error) {
	if token == "" {
		return nil, ErrMissingToken
	}

	secrets := []string{p.conf.Secret}
	if p.conf.PrevSecret != "" {
		secrets = append(secrets, p.conf.PrevSecret)
	}

	var lastErr error
	for _, secret := range secrets {
		claims, err := p.parseWithSecret(token, secret)
		if err == nil {
			return claims, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("%w: %v", ErrInvalidToken, lastErr)
}

func (p *Parser) parseWithSecret(token, secret string) (*Claims, error) {
	options := []jwtlib.ParserOption{
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		// Generate always sets exp, so require it here as well; this also rejects
		// tokens minted elsewhere without an expiry.
		jwtlib.WithExpirationRequired(),
	}
	if p.conf.Issuer != "" {
		// Without this check tokens signed with the same secret by another
		// service (a different iss) would be accepted.
		options = append(options, jwtlib.WithIssuer(p.conf.Issuer))
	}

	parsed, err := jwtlib.Parse(token, func(*jwtlib.Token) (any, error) {
		return []byte(secret), nil
	}, options...)
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errInvalid
	}

	mapClaims, ok := parsed.Claims.(jwtlib.MapClaims)
	if !ok {
		return nil, ErrNoClaims
	}

	claims := &Claims{Data: make(map[string]any, len(mapClaims))}
	for key, value := range mapClaims {
		switch key {
		case claimSubject:
			claims.Subject, _ = value.(string)
		case claimIssuer:
			claims.Issuer, _ = value.(string)
		case claimIssued:
			claims.IssuedAt = unixTime(value)
		case claimExpires:
			claims.ExpiresAt = unixTime(value)
		default:
			claims.Data[key] = value
		}
	}

	if claims.Subject == "" {
		return nil, ErrNoClaims
	}
	return claims, nil
}

var errInvalid = errors.New("token: token is not valid")

func unixTime(value any) time.Time {
	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0)
	case int64:
		return time.Unix(typed, 0)
	default:
		return time.Time{}
	}
}
