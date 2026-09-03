package jwt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestJWT(t *testing.T) *JWT {
	t.Helper()
	j, err := New(Config{
		Secret:             "test-secret-key",
		Issuer:             "test-app",
		Audience:           []string{"web"},
		AccessTokenExpire:  1 * time.Hour,
		RefreshTokenExpire: 24 * time.Hour,
		Algorithm:          AlgorithmHS256,
	})
	require.NoError(t, err)
	return j
}

// --- fillDefault 测试 ---

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{Secret: "key"})

	assert.Equal(t, "key", c.Secret)
	assert.Equal(t, "", c.Issuer)
	assert.Nil(t, c.Audience)
	assert.Equal(t, 2*time.Hour, c.AccessTokenExpire)
	assert.Equal(t, 168*time.Hour, c.RefreshTokenExpire)
	assert.Equal(t, AlgorithmHS256, c.Algorithm)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := fillDefault(Config{
		Secret:             "my-secret",
		Issuer:             "my-app",
		Audience:           []string{"web", "app"},
		AccessTokenExpire:  30 * time.Minute,
		RefreshTokenExpire: 7 * 24 * time.Hour,
		Algorithm:          AlgorithmHS512,
	})

	assert.Equal(t, "my-secret", c.Secret)
	assert.Equal(t, "my-app", c.Issuer)
	assert.Equal(t, []string{"web", "app"}, c.Audience)
	assert.Equal(t, 30*time.Minute, c.AccessTokenExpire)
	assert.Equal(t, 7*24*time.Hour, c.RefreshTokenExpire)
	assert.Equal(t, AlgorithmHS512, c.Algorithm)
}

// --- signingMethod 测试 ---

func TestSigningMethod(t *testing.T) {
	tests := []struct {
		alg    Algorithm
		expect string
	}{
		{AlgorithmHS256, "HS256"},
		{AlgorithmHS384, "HS384"},
		{AlgorithmHS512, "HS512"},
	}

	for _, tt := range tests {
		method, err := signingMethod(tt.alg)
		require.NoError(t, err)
		assert.Equal(t, tt.expect, method.Alg())
	}
}

func TestSigningMethod_Unsupported(t *testing.T) {
	_, err := signingMethod("RS256")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

// --- New / MustNew 测试 ---

func TestNew(t *testing.T) {
	j, err := New(Config{Secret: "key"})
	require.NoError(t, err)
	assert.NotNil(t, j)
	assert.Equal(t, "key", j.Config().Secret)
}

func TestNew_EmptySecret(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSecretEmpty)
}

func TestNew_UnsupportedAlgorithm(t *testing.T) {
	_, err := New(Config{Secret: "key", Algorithm: "RS256"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestMustNew(t *testing.T) {
	j := MustNew(Config{Secret: "key"})
	assert.NotNil(t, j)
}

func TestMustNew_Panic(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(Config{})
	})
}

// --- GenerateToken 测试 ---

func TestGenerateToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateToken(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
	}, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGenerateToken_HS384(t *testing.T) {
	j, err := New(Config{
		Secret:    "key",
		Algorithm: AlgorithmHS384,
	})
	require.NoError(t, err)

	token, err := j.GenerateToken(Claims{ClaimKeyUserID: "123"}, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGenerateToken_HS512(t *testing.T) {
	j, err := New(Config{
		Secret:    "key",
		Algorithm: AlgorithmHS512,
	})
	require.NoError(t, err)

	token, err := j.GenerateToken(Claims{ClaimKeyUserID: "123"}, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

// --- GenerateAccessToken / GenerateRefreshToken 测试 ---

func TestGenerateAccessToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
		ClaimKeyRole:     "admin",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := j.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims[ClaimKeyUserID])
	assert.Equal(t, "alice", claims[ClaimKeyUsername])
	assert.Equal(t, "admin", claims[ClaimKeyRole])
	assert.Equal(t, TokenTypeAccess, claims[ClaimKeyTokenType])
}

func TestGenerateRefreshToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateRefreshToken(Claims{
		ClaimKeyUserID: "user-123",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := j.ParseRefreshToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims[ClaimKeyUserID])
	assert.Equal(t, TokenTypeRefresh, claims[ClaimKeyTokenType])
}

// --- GenerateTokenPair 测试 ---

func TestGenerateTokenPair(t *testing.T) {
	j := newTestJWT(t)

	pair, err := j.GenerateTokenPair(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
		ClaimKeyRole:     "admin",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.NotEqual(t, pair.AccessToken, pair.RefreshToken)
	assert.True(t, pair.ExpiresAt > time.Now().Unix())
}

// --- ParseToken 测试 ---

func TestParseToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
	})
	require.NoError(t, err)

	claims, err := j.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims[ClaimKeyUserID])
	assert.Equal(t, "alice", claims[ClaimKeyUsername])
	assert.Equal(t, "test-app", claims[ClaimKeyIssuer])
}

func TestParseToken_Invalid(t *testing.T) {
	j := newTestJWT(t)

	_, err := j.ParseToken("invalid.token.string")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseToken_WrongSecret(t *testing.T) {
	j1, err := New(Config{Secret: "secret-1"})
	require.NoError(t, err)

	j2, err := New(Config{Secret: "secret-2"})
	require.NoError(t, err)

	token, err := j1.GenerateAccessToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	_, err = j2.ParseToken(token)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseToken_Expired(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateToken(Claims{ClaimKeyUserID: "123"}, -1*time.Hour)
	require.NoError(t, err)

	_, err = j.ParseToken(token)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExpiredToken)
}

// --- ParseAccessToken 测试 ---

func TestParseAccessToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	claims, err := j.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, TokenTypeAccess, claims[ClaimKeyTokenType])
}

func TestParseAccessToken_WithRefreshToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateRefreshToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	_, err = j.ParseAccessToken(token)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// --- ParseRefreshToken 测试 ---

func TestParseRefreshToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateRefreshToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	claims, err := j.ParseRefreshToken(token)
	require.NoError(t, err)
	assert.Equal(t, TokenTypeRefresh, claims[ClaimKeyTokenType])
}

func TestParseRefreshToken_WithAccessToken(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	_, err = j.ParseRefreshToken(token)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotRefreshToken)
}

// --- RefreshToken 测试 ---

func TestRefreshToken(t *testing.T) {
	j := newTestJWT(t)

	pair, err := j.GenerateTokenPair(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
		ClaimKeyRole:     "admin",
	})
	require.NoError(t, err)

	newPair, err := j.RefreshToken(pair.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newPair.AccessToken)
	assert.NotEmpty(t, newPair.RefreshToken)

	// 验证新令牌中的用户信息一致
	claims, err := j.ParseAccessToken(newPair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims[ClaimKeyUserID])
	assert.Equal(t, "alice", claims[ClaimKeyUsername])
	assert.Equal(t, "admin", claims[ClaimKeyRole])
}

func TestRefreshToken_WithAccessToken(t *testing.T) {
	j := newTestJWT(t)

	pair, err := j.GenerateTokenPair(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	_, err = j.RefreshToken(pair.AccessToken)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotRefreshToken)
}

func TestRefreshToken_Expired(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateToken(Claims{
		ClaimKeyUserID:    "123",
		ClaimKeyTokenType: TokenTypeRefresh,
	}, -1*time.Hour)
	require.NoError(t, err)

	_, err = j.RefreshToken(token)
	require.Error(t, err)
}

// --- MapClaims 自由扩展测试 ---

func TestMapClaims_CustomFields(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{
		ClaimKeyUserID: "user-456",
		"company":      "acme",
		ClaimKeyScopes: []string{"read", "write"},
		"metadata":     map[string]any{"department": "engineering"},
	})
	require.NoError(t, err)

	claims, err := j.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-456", claims[ClaimKeyUserID])
	assert.Equal(t, "acme", claims["company"])
	assert.Equal(t, []any{"read", "write"}, claims[ClaimKeyScopes])
	meta, ok := claims["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "engineering", meta["department"])
}

// --- 辅助函数测试 ---

func TestCopyClaims(t *testing.T) {
	src := Claims{"a": 1, "b": "hello"}
	dst := copyClaims(src)

	assert.Equal(t, src, dst)

	// 修改 dst 不影响 src
	dst["a"] = 2
	assert.Equal(t, 1, src["a"])
}

func TestExtractBusinessClaims(t *testing.T) {
	claims := Claims{
		ClaimKeyUserID:         "123",
		ClaimKeyUsername:       "alice",
		ClaimKeyIssuer:         "test-app",
		ClaimKeyAudience:       []string{"web"},
		ClaimKeyExpirationTime: int64(1234567890),
		ClaimKeyIssuedAt:       int64(1234567890),
		ClaimKeyNotBefore:      int64(1234567890),
		ClaimKeyTokenType:      "access",
	}

	business := extractBusinessClaims(claims)

	assert.Equal(t, "123", business[ClaimKeyUserID])
	assert.Equal(t, "alice", business[ClaimKeyUsername])
	assert.NotContains(t, business, ClaimKeyIssuer)
	assert.NotContains(t, business, ClaimKeyAudience)
	assert.NotContains(t, business, ClaimKeyExpirationTime)
	assert.NotContains(t, business, ClaimKeyIssuedAt)
	assert.NotContains(t, business, ClaimKeyNotBefore)
	assert.NotContains(t, business, ClaimKeyTokenType)
}

// --- 错误常量测试 ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "jwt: invalid token", ErrInvalidToken.Error())
	assert.Equal(t, "jwt: token is expired", ErrExpiredToken.Error())
	assert.Equal(t, "jwt: token is not a refresh token", ErrNotRefreshToken.Error())
	assert.Equal(t, "jwt: secret is empty", ErrSecretEmpty.Error())
}
