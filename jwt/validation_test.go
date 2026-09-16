package jwt

import (
	"testing"
	"time"

	stdjwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers regression tests for the issuer/audience (iss/aud) and
// algorithm validation.
//
// Historic defect: ParseToken validated only the signature and the expiry, so
// Config.Issuer / Config.Audience were written at signing time but never
// validated. Different applications sharing the same secret (or multi-tenant
// setups) could therefore accept each other's tokens — a token with
// iss=app-a/aud=tenant-a was accepted by an instance configured with
// app-b/tenant-b, allowing cross-tenant privilege escalation.

const crossTenantSecret = "shared-secret"

// newJWTWithIdentity builds an instance with the given iss/aud.
func newJWTWithIdentity(t *testing.T, issuer string, audience ...string) *JWT {
	t.Helper()
	j, err := New(Config{
		Secret:             crossTenantSecret,
		Issuer:             issuer,
		Audience:           audience,
		AccessTokenExpire:  1 * time.Hour,
		RefreshTokenExpire: 24 * time.Hour,
	})
	require.NoError(t, err)
	return j
}

// TestParseToken_RejectsForeignIssuer is a regression test: instances with
// different Issuers must not accept each other's tokens.
func TestParseToken_RejectsForeignIssuer(t *testing.T) {
	appA := newJWTWithIdentity(t, "app-a", "tenant-a")
	appB := newJWTWithIdentity(t, "app-b", "tenant-b")

	token, err := appA.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// Its own tokens still work
	claims, err := appA.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "1", claims["user_id"])

	// Other applications must reject it
	_, err = appB.ParseAccessToken(token)
	require.Error(t, err, "a token issued for app-a must not be accepted by app-b")
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Contains(t, err.Error(), "issuer")
}

// TestParseToken_RejectsForeignAudience is a regression test: a mismatched
// audience must be rejected.
func TestParseToken_RejectsForeignAudience(t *testing.T) {
	tenantA := newJWTWithIdentity(t, "app", "tenant-a")
	tenantB := newJWTWithIdentity(t, "app", "tenant-b")

	token, err := tenantA.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	_, err = tenantB.ParseAccessToken(token)
	require.Error(t, err, "a token for tenant-a must not be accepted for tenant-b")
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Contains(t, err.Error(), "audience")
}

// TestParseToken_AcceptsOverlappingAudience verifies that overlapping audiences
// pass (WithAudience semantics: the token aud only needs to contain one of the
// configured values).
func TestParseToken_AcceptsOverlappingAudience(t *testing.T) {
	issuer := newJWTWithIdentity(t, "app", "web", "app")
	verifier := newJWTWithIdentity(t, "app", "app", "internal")

	token, err := issuer.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	_, err = verifier.ParseAccessToken(token)
	assert.NoError(t, err, "overlapping audience must be accepted")
}

// TestParseToken_IssuerAudienceOptional verifies that iss/aud are not validated
// when they are not configured (keeping the existing "secret-only validation"
// usage working).
func TestParseToken_IssuerAudienceOptional(t *testing.T) {
	// The signer carries iss/aud
	withIdentity := newJWTWithIdentity(t, "app-a", "tenant-a")
	token, err := withIdentity.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// The verifier only configures the secret (no iss/aud) → accepted
	bare, err := New(Config{Secret: crossTenantSecret})
	require.NoError(t, err)
	claims, err := bare.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "1", claims["user_id"])

	// Reverse: the signer has no iss/aud while the verifier configures iss →
	// the token has no iss and must be rejected
	noIdentity, err := New(Config{Secret: crossTenantSecret})
	require.NoError(t, err)
	bareToken, err := noIdentity.GenerateAccessToken(Claims{"user_id": "2"})
	require.NoError(t, err)

	strict := newJWTWithIdentity(t, "app-a")
	_, err = strict.ParseAccessToken(bareToken)
	assert.Error(t, err, "when Issuer is configured, a token without iss must be rejected")
}

// TestParseToken_WrongAlgorithmRejected verifies that a token with a mismatched
// algorithm is rejected.
func TestParseToken_WrongAlgorithmRejected(t *testing.T) {
	hs256, err := New(Config{Secret: crossTenantSecret, Algorithm: AlgorithmHS256})
	require.NoError(t, err)
	hs512, err := New(Config{Secret: crossTenantSecret, Algorithm: AlgorithmHS512})
	require.NoError(t, err)

	token256, err := hs256.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// An HS512 instance must not accept an HS256 token (even with the same secret)
	_, err = hs512.ParseAccessToken(token256)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseToken_AlgNoneRejected verifies that unsigned alg=none tokens are
// rejected.
func TestParseToken_AlgNoneRejected(t *testing.T) {
	j := newJWTWithIdentity(t, "app", "web")

	unsigned := stdjwt.NewWithClaims(stdjwt.SigningMethodNone, Claims{
		ClaimKeyIssuer:         "app",
		ClaimKeyAudience:       []string{"web"},
		ClaimKeyExpirationTime: time.Now().Add(time.Hour).Unix(),
	})
	token, err := unsigned.SignedString(stdjwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = j.ParseAccessToken(token)
	assert.Error(t, err, "alg=none tokens must be rejected")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseToken_AlgConfusionRejected verifies that a token re-signed with HS512
// while claiming alg=HS256 is rejected.
// Such constructions are used to bypass implementations that only trust
// Header["alg"].
func TestParseToken_AlgConfusionRejected(t *testing.T) {
	verifier, err := New(Config{
		Secret:    crossTenantSecret,
		Algorithm: AlgorithmHS256,
		Issuer:    "app",
	})
	require.NoError(t, err)

	// Actually sign with HS512 but change the header alg to HS256
	claims := Claims{
		ClaimKeyIssuer:         "app",
		ClaimKeyExpirationTime: time.Now().Add(time.Hour).Unix(),
	}
	fake := stdjwt.NewWithClaims(stdjwt.SigningMethodHS512, claims)
	fake.Header["alg"] = string(AlgorithmHS256)
	evil, err := fake.SignedString([]byte(crossTenantSecret))
	require.NoError(t, err)

	_, err = verifier.ParseAccessToken(evil)
	assert.Error(t, err, "algorithm confusion must be rejected")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestGenerateToken_ConfigOverridesCallerIssuerAudience verifies that iss/aud
// supplied by the caller are overwritten by the configured values, so the
// signing issuer/audience cannot be forged.
//
// This is an important security property: GenerateToken unconditionally writes
// config.Issuer / config.Audience, so a caller cannot disguise its own token as
// another application's token through claims.
func TestGenerateToken_ConfigOverridesCallerIssuerAudience(t *testing.T) {
	appA := newJWTWithIdentity(t, "app-a", "tenant-a")
	appB := newJWTWithIdentity(t, "app-b", "tenant-b")

	// The caller tries to forge iss/aud as app-b / tenant-b
	token, err := appA.GenerateAccessToken(Claims{
		ClaimKeyIssuer:   "app-b",
		ClaimKeyAudience: []string{"tenant-b"},
		"user_id":        "1",
	})
	require.NoError(t, err)

	// app-a validates it (iss is actually app-a)
	claims, err := appA.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "app-a", claims[ClaimKeyIssuer], "caller-supplied iss must be overwritten")
	assert.Equal(t, []any{"tenant-a"}, claims[ClaimKeyAudience],
		"caller-supplied aud must be overwritten")

	// app-b must reject it (the forgery failed)
	_, err = appB.ParseAccessToken(token)
	require.Error(t, err, "forging iss/aud via claims must not work")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseToken_CrossInstanceRejectedBothWays verifies that two instances with
// different identities reject each other's tokens.
func TestParseToken_CrossInstanceRejectedBothWays(t *testing.T) {
	appA := newJWTWithIdentity(t, "app-a", "tenant-a")
	appB := newJWTWithIdentity(t, "app-b", "tenant-b")

	tokenA, err := appA.GenerateAccessToken(Claims{"user_id": "a"})
	require.NoError(t, err)
	tokenB, err := appB.GenerateAccessToken(Claims{"user_id": "b"})
	require.NoError(t, err)

	_, err = appB.ParseAccessToken(tokenA)
	assert.Error(t, err, "app-b must reject app-a's token")

	_, err = appA.ParseAccessToken(tokenB)
	assert.Error(t, err, "app-a must reject app-b's token")

	// Each instance still validates its own token normally
	_, err = appA.ParseAccessToken(tokenA)
	assert.NoError(t, err)
	_, err = appB.ParseAccessToken(tokenB)
	assert.NoError(t, err)
}
