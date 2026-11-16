package token

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimsContext(t *testing.T) {
	ctx := context.Background()
	_, ok := ClaimsFrom(ctx)
	assert.False(t, ok)

	claims := &Claims{Subject: "alice"}
	ctx = WithClaims(ctx, claims)
	got, ok := ClaimsFrom(ctx)
	require.True(t, ok)
	assert.Same(t, claims, got)

	// A typed nil pointer is still a stored value; callers can distinguish it from
	// "not present" through the boolean.
	ctx = WithClaims(context.Background(), nil)
	got, ok = ClaimsFrom(ctx)
	assert.True(t, ok)
	assert.Nil(t, got)
}

func TestUnixTimeBranches(t *testing.T) {
	assert.Equal(t, time.Unix(1, 0), unixTime(float64(1)))
	assert.Equal(t, time.Unix(2, 0), unixTime(int64(2)))
	assert.True(t, unixTime("not-a-number").IsZero())
	assert.True(t, unixTime(nil).IsZero())
}

// TestClaimsContextToleratesNilContext keeps the helpers safe for standalone
// handlers that forgot to pass a context.
func TestClaimsContextToleratesNilContext(t *testing.T) {
	_, ok := ClaimsFrom(nil)
	assert.False(t, ok)

	ctx := WithClaims(nil, &Claims{Subject: "alice"})
	got, ok := ClaimsFrom(ctx)
	require.True(t, ok)
	assert.Equal(t, "alice", got.Subject)
}
