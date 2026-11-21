package token

import (
	"context"
	"testing"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithClaimsStoresCustomClaims(t *testing.T) {
	ctx := WithClaims(context.Background(), jwtlib.MapClaims{
		"uid":   "user-1",
		"scope": "read",
		"sub":   "ignored",
		"exp":   float64(0),
		"iss":   "demo",
	})

	uid, ok := Claim(ctx, "uid")
	require.True(t, ok)
	assert.Equal(t, "user-1", uid)

	scope, ok := Claim(ctx, "scope")
	require.True(t, ok)
	assert.Equal(t, "read", scope)

	// Standard claims describe the token, not the caller.
	for _, name := range []string{"sub", "exp", "iss"} {
		if _, ok := Claim(ctx, name); ok {
			t.Errorf("standard claim %q must not be stored", name)
		}
	}
}

func TestClaimWithoutValue(t *testing.T) {
	_, ok := Claim(context.Background(), "uid")
	assert.False(t, ok)

	_, ok = Claim(nil, "uid")
	assert.False(t, ok)
}

// TestWithClaimsToleratesNilContext keeps the helper safe for standalone
// handlers that forgot to pass a context.
func TestWithClaimsToleratesNilContext(t *testing.T) {
	ctx := WithClaims(nil, jwtlib.MapClaims{"uid": "alice"})
	uid, ok := Claim(ctx, "uid")
	require.True(t, ok)
	assert.Equal(t, "alice", uid)
}
