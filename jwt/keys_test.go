package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Claim key constant tests ---

func TestClaimKeyConstants(t *testing.T) {
	// Standard claims
	assert.Equal(t, "iss", ClaimKeyIssuer)
	assert.Equal(t, "sub", ClaimKeySubject)
	assert.Equal(t, "aud", ClaimKeyAudience)
	assert.Equal(t, "exp", ClaimKeyExpirationTime)
	assert.Equal(t, "nbf", ClaimKeyNotBefore)
	assert.Equal(t, "iat", ClaimKeyIssuedAt)
	assert.Equal(t, "jti", ClaimKeyJWTID)

	// Custom claims
	assert.Equal(t, "token_type", ClaimKeyTokenType)

	// Common business claims
	assert.Equal(t, "user_id", ClaimKeyUserID)
	assert.Equal(t, "username", ClaimKeyUsername)
	assert.Equal(t, "role", ClaimKeyRole)
	assert.Equal(t, "permissions", ClaimKeyPermissions)
	assert.Equal(t, "scopes", ClaimKeyScopes)
}
