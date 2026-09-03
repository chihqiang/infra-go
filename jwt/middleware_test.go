package jwt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Context 集成测试 ---

func TestWithClaims(t *testing.T) {
	ctx := context.Background()
	claims := Claims{ClaimKeyUserID: "123", ClaimKeyRole: "admin"}

	newCtx := WithClaims(ctx, claims)

	// 原始 context 不受影响
	assert.Nil(t, ClaimsFromContext(ctx))

	// 新 context 有 claims
	extracted := ClaimsFromContext(newCtx)
	require.NotNil(t, extracted)
	assert.Equal(t, "123", extracted[ClaimKeyUserID])
	assert.Equal(t, "admin", extracted[ClaimKeyRole])
}

func TestClaimsFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, ClaimsFromContext(ctx))
}

// --- AuthMiddleware 测试 ---

// headerTokenExtractor 从指定请求头提取 token，用于测试。
func headerTokenExtractor(headerName string) func(*http.Request) string {
	return func(r *http.Request) string {
		return r.Header.Get(headerName)
	}
}

func TestAuthMiddleware_Success(t *testing.T) {
	j := newTestJWT(t)

	token, err := j.GenerateAccessToken(Claims{
		ClaimKeyUserID:   "user-123",
		ClaimKeyUsername: "alice",
		ClaimKeyRole:     "admin",
	})
	require.NoError(t, err)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))

	var (
		called       bool
		gotUserID    any
		gotCtxUserID any
		ctxClaims    Claims
	)
	handler := mw(func(w http.ResponseWriter, r *http.Request) {
		called = true
		ctxClaims = ClaimsFromContext(r.Context())
		gotUserID = ctxClaims[ClaimKeyUserID]
		// 验证逐个注入的 context value
		gotCtxUserID = r.Context().Value(claimCtxKey(ClaimKeyUserID))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("X-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "user-123", gotUserID)
	assert.Equal(t, "user-123", gotCtxUserID)
	require.NotNil(t, ctxClaims)
	assert.Equal(t, "alice", ctxClaims[ClaimKeyUsername])
	assert.Equal(t, "admin", ctxClaims[ClaimKeyRole])
	// 标准声明和 token_type 不应注入
	assert.NotContains(t, ctxClaims, ClaimKeyIssuer)
	assert.NotContains(t, ctxClaims, ClaimKeyTokenType)
}

func TestAuthMiddleware_TokenMissing(t *testing.T) {
	j := newTestJWT(t)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))

	var called bool
	handler := mw(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), msgTokenMissing)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	j := newTestJWT(t)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))

	var called bool
	handler := mw(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", "invalid.token.string")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), msgInvalidToken)
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	j := newTestJWT(t)

	// 生成已过期的 access token
	token, err := j.GenerateToken(Claims{
		ClaimKeyUserID:    "123",
		ClaimKeyTokenType: TokenTypeAccess,
	}, -1*time.Hour)
	require.NoError(t, err)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))

	var called bool
	handler := mw(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), msgTokenExpired)
}

func TestAuthMiddleware_RefreshTokenAsAccess(t *testing.T) {
	j := newTestJWT(t)

	// 用 refresh token 当作 access token，应被拒绝
	token, err := j.GenerateRefreshToken(Claims{ClaimKeyUserID: "123"})
	require.NoError(t, err)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))

	var called bool
	handler := mw(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), msgInvalidToken)
}
