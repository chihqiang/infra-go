package token

import (
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const testSecret = "a-sufficiently-long-secret"

func newParser(t *testing.T, conf Config) *Parser {
	t.Helper()

	if conf.Secret == "" {
		conf.Secret = testSecret
	}
	parser, err := NewParser(conf)
	if err != nil {
		t.Fatal(err)
	}
	return parser
}

func TestNewRejectsShortSecret(t *testing.T) {
	if _, err := NewParser(Config{Secret: "short"}); !errors.Is(err, ErrShortSecret) {
		t.Fatalf("error = %v, want ErrShortSecret", err)
	}
}

func TestGenerateRejectsEmptyClaims(t *testing.T) {
	if _, err := newParser(t, Config{}).Generate(nil); !errors.Is(err, ErrNoClaims) {
		t.Fatalf("error = %v, want ErrNoClaims", err)
	}
}

func TestGenerateAndParseRoundTrip(t *testing.T) {
	parser := newParser(t, Config{})

	signed, err := parser.Generate(jwtlib.MapClaims{
		"sub":  "user-1",
		"iss":  "infra-go",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"role": "admin",
	})
	if err != nil {
		t.Fatal(err)
	}

	claims, err := parser.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "user-1" {
		t.Fatalf("subject = %v, want user-1", claims["sub"])
	}
	if claims["iss"] != "infra-go" {
		t.Fatalf("issuer = %v, want infra-go", claims["iss"])
	}
	if claims["role"] != "admin" {
		t.Fatalf("custom claim role = %v", claims["role"])
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("exp = %v, want a number", claims["exp"])
	}
	if remaining := time.Until(time.Unix(int64(exp), 0)); remaining <= 0 || remaining > time.Hour {
		t.Fatalf("expires in %v, want within an hour", remaining)
	}
}

func TestParseRejectsExpiredAndTamperedTokens(t *testing.T) {
	parser := newParser(t, Config{})

	expired, err := parser.Generate(jwtlib.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(expired); err == nil {
		t.Fatal("expired token should fail to parse")
	}

	valid := newParser(t, Config{})
	token, err := valid.Generate(jwtlib.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := valid.Parse(token + "tampered"); err == nil {
		t.Fatal("tampered token should fail to parse")
	}
	if _, err := valid.Parse(""); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("error = %v, want ErrMissingToken", err)
	}
}

func TestParseAcceptsPreviousSecret(t *testing.T) {
	old := newParser(t, Config{})
	token, err := old.Generate(jwtlib.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	rotated := newParser(t, Config{Secret: "the-new-signing-secret", PrevSecret: testSecret})
	if _, err := rotated.Parse(token); err != nil {
		t.Fatalf("previous secret should still verify: %v", err)
	}
}

func TestNewRejectsShortPreviousSecret(t *testing.T) {
	_, err := NewParser(Config{Secret: testSecret, PrevSecret: "short"})
	if !errors.Is(err, ErrShortPrevSecret) {
		t.Fatalf("error = %v, want ErrShortPrevSecret", err)
	}
}

// TestParseKeepsIssuerClaim documents the contract: the issuer is a signing
// detail, so Parse reports it but does not reject a different iss.
func TestParseKeepsIssuerClaim(t *testing.T) {
	parser := newParser(t, Config{})
	signed, err := parser.Generate(jwtlib.MapClaims{
		"sub": "user-1",
		"iss": "service-a",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	claims, err := parser.Parse(signed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims["iss"] != "service-a" {
		t.Fatalf("issuer = %v, want service-a", claims["iss"])
	}
}

func TestParseRejectsTokenWithoutExpiry(t *testing.T) {
	parser := newParser(t, Config{})

	raw, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, jwtlib.MapClaims{
		"sub": "user-1",
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}
