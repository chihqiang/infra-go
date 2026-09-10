package jwt

import (
	"testing"
	"time"

	stdjwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖签发行/受众（iss/aud）与算法校验的回归测试。
//
// 历史缺陷：ParseToken 只校验签名与过期时间，Config.Issuer / Config.Audience
// 仅在签发时写入、从不参与校验。共享同一密钥的不同应用（或多租户场景）因此可以
// 互相接受对方签发的令牌 —— iss=app-a/aud=tenant-a 的令牌会被配置为
// app-b/tenant-b 的实例接受，构成跨租户越权。

const crossTenantSecret = "shared-secret"

// newJWTWithIdentity 构造带指定 iss/aud 的实例。
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

// TestParseToken_RejectsForeignIssuer 回归测试：不同 Issuer 的实例不得接受彼此的令牌。
func TestParseToken_RejectsForeignIssuer(t *testing.T) {
	appA := newJWTWithIdentity(t, "app-a", "tenant-a")
	appB := newJWTWithIdentity(t, "app-b", "tenant-b")

	token, err := appA.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// 自己签发的仍可用
	claims, err := appA.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "1", claims["user_id"])

	// 其他应用必须拒绝
	_, err = appB.ParseAccessToken(token)
	require.Error(t, err, "a token issued for app-a must not be accepted by app-b")
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Contains(t, err.Error(), "issuer")
}

// TestParseToken_RejectsForeignAudience 回归测试：受众不匹配时必须拒绝。
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

// TestParseToken_AcceptsOverlappingAudience 验证受众存在交集时可通过
// （WithAudience 语义：令牌 aud 包含任一配置值即可）。
func TestParseToken_AcceptsOverlappingAudience(t *testing.T) {
	issuer := newJWTWithIdentity(t, "app", "web", "app")
	verifier := newJWTWithIdentity(t, "app", "app", "internal")

	token, err := issuer.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	_, err = verifier.ParseAccessToken(token)
	assert.NoError(t, err, "overlapping audience must be accepted")
}

// TestParseToken_IssuerAudienceOptional 验证未配置 iss/aud 时不做对应校验
// （保持"仅用密钥校验"的既有用法可用）。
func TestParseToken_IssuerAudienceOptional(t *testing.T) {
	// 签发方带 iss/aud
	withIdentity := newJWTWithIdentity(t, "app-a", "tenant-a")
	token, err := withIdentity.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// 校验方只配密钥（不配 iss/aud）→ 接受
	bare, err := New(Config{Secret: crossTenantSecret})
	require.NoError(t, err)
	claims, err := bare.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "1", claims["user_id"])

	// 反向：签发方不带 iss/aud，校验方配置了 iss → 令牌无 iss，必须拒绝
	noIdentity, err := New(Config{Secret: crossTenantSecret})
	require.NoError(t, err)
	bareToken, err := noIdentity.GenerateAccessToken(Claims{"user_id": "2"})
	require.NoError(t, err)

	strict := newJWTWithIdentity(t, "app-a")
	_, err = strict.ParseAccessToken(bareToken)
	assert.Error(t, err, "when Issuer is configured, a token without iss must be rejected")
}

// TestParseToken_WrongAlgorithmRejected 验证算法不匹配的令牌被拒绝。
func TestParseToken_WrongAlgorithmRejected(t *testing.T) {
	hs256, err := New(Config{Secret: crossTenantSecret, Algorithm: AlgorithmHS256})
	require.NoError(t, err)
	hs512, err := New(Config{Secret: crossTenantSecret, Algorithm: AlgorithmHS512})
	require.NoError(t, err)

	token256, err := hs256.GenerateAccessToken(Claims{"user_id": "1"})
	require.NoError(t, err)

	// HS512 实例不得接受 HS256 令牌（即使密钥相同）
	_, err = hs512.ParseAccessToken(token256)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseToken_AlgNoneRejected 验证 alg=none 的未签名令牌被拒绝。
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

// TestParseToken_AlgConfusionRejected 验证用 HS512 重签、但声称 alg=HS256 的令牌被拒绝。
// 这类构造用于绕过只信任 Header["alg"] 的实现。
func TestParseToken_AlgConfusionRejected(t *testing.T) {
	verifier, err := New(Config{
		Secret:    crossTenantSecret,
		Algorithm: AlgorithmHS256,
		Issuer:    "app",
	})
	require.NoError(t, err)

	// 用 HS512 实际签名，但把 header 的 alg 改成 HS256
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

// TestGenerateToken_ConfigOverridesCallerIssuerAudience 验证调用方传入的 iss/aud
// 会被配置值覆盖，无法伪造签发行/受众。
//
// 这是重要的安全属性：GenerateToken 无条件写入 config.Issuer / config.Audience，
// 因此调用方无法通过 claims 把自己的令牌伪装成其他应用的令牌。
func TestGenerateToken_ConfigOverridesCallerIssuerAudience(t *testing.T) {
	appA := newJWTWithIdentity(t, "app-a", "tenant-a")
	appB := newJWTWithIdentity(t, "app-b", "tenant-b")

	// 调用方试图把 iss/aud 伪造成 app-b / tenant-b
	token, err := appA.GenerateAccessToken(Claims{
		ClaimKeyIssuer:   "app-b",
		ClaimKeyAudience: []string{"tenant-b"},
		"user_id":        "1",
	})
	require.NoError(t, err)

	// app-a 校验通过（iss 实际为 app-a）
	claims, err := appA.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "app-a", claims[ClaimKeyIssuer], "caller-supplied iss must be overwritten")
	assert.Equal(t, []any{"tenant-a"}, claims[ClaimKeyAudience],
		"caller-supplied aud must be overwritten")

	// app-b 必须拒绝（伪造失败）
	_, err = appB.ParseAccessToken(token)
	require.Error(t, err, "forging iss/aud via claims must not work")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseToken_CrossInstanceRejectedBothWays 验证两个不同身份的实例互相拒绝对方的令牌。
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

	// 各自校验自己的令牌仍然正常
	_, err = appA.ParseAccessToken(tokenA)
	assert.NoError(t, err)
	_, err = appB.ParseAccessToken(tokenB)
	assert.NoError(t, err)
}
