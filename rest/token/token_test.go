package token

import (
	"errors"
	"testing"
	"time"
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

func TestNewDefaultsExpireToADay(t *testing.T) {
	parser := newParser(t, Config{})
	if got := parser.Config().Expire; got != 24*time.Hour {
		t.Fatalf("expire = %v, want 24h", got)
	}
}

func TestGenerateAndParseRoundTrip(t *testing.T) {
	parser := newParser(t, Config{Issuer: "infra-go", Expire: time.Hour})

	token, err := parser.Generate("user-1", map[string]any{"role": "admin", "sub": "ignored"})
	if err != nil {
		t.Fatal(err)
	}

	claims, err := parser.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("subject = %q, want user-1", claims.Subject)
	}
	if claims.Issuer != "infra-go" {
		t.Fatalf("issuer = %q, want infra-go", claims.Issuer)
	}
	if claims.Data["role"] != "admin" {
		t.Fatalf("custom claim role = %v", claims.Data["role"])
	}
	if _, overridden := claims.Data["sub"]; overridden {
		t.Fatal("custom data must not override the subject claim")
	}
	if remaining := time.Until(claims.ExpiresAt); remaining <= 0 || remaining > time.Hour {
		t.Fatalf("expires in %v, want within an hour", remaining)
	}
}

func TestParseRejectsExpiredAndTamperedTokens(t *testing.T) {
	// Bypass New to mint an already-expired token.
	expiredManager := &Parser{conf: Config{Secret: testSecret, Expire: -time.Minute}}
	expired, err := expiredManager.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newParser(t, Config{}).Parse(expired); err == nil {
		t.Fatal("expired token should fail to parse")
	}

	valid := newParser(t, Config{Expire: time.Hour})
	token, err := valid.Generate("user-1", nil)
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
	old := newParser(t, Config{Expire: time.Hour})
	token, err := old.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	rotated := newParser(t, Config{Secret: "the-new-signing-secret", PrevSecret: testSecret, Expire: time.Hour})
	if _, err := rotated.Parse(token); err != nil {
		t.Fatalf("previous secret should still verify: %v", err)
	}
}
