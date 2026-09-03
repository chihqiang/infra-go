package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- 声明键常量测试 ---

func TestClaimKeyConstants(t *testing.T) {
	// 标准声明
	assert.Equal(t, "iss", ClaimKeyIssuer)
	assert.Equal(t, "sub", ClaimKeySubject)
	assert.Equal(t, "aud", ClaimKeyAudience)
	assert.Equal(t, "exp", ClaimKeyExpirationTime)
	assert.Equal(t, "nbf", ClaimKeyNotBefore)
	assert.Equal(t, "iat", ClaimKeyIssuedAt)
	assert.Equal(t, "jti", ClaimKeyJWTID)

	// 自定义声明
	assert.Equal(t, "token_type", ClaimKeyTokenType)

	// 常用业务声明
	assert.Equal(t, "user_id", ClaimKeyUserID)
	assert.Equal(t, "username", ClaimKeyUsername)
	assert.Equal(t, "role", ClaimKeyRole)
	assert.Equal(t, "permissions", ClaimKeyPermissions)
	assert.Equal(t, "scopes", ClaimKeyScopes)
}
