package jwt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Context integration tests ---

func TestWithClaims(t *testing.T) {
	ctx := context.Background()
	claims := Claims{ClaimKeyUserID: "123", ClaimKeyRole: "admin"}

	newCtx := WithClaims(ctx, claims)

	// The original context is unaffected
	assert.Nil(t, ClaimsFromContext(ctx))

	// The new context carries claims
	extracted := ClaimsFromContext(newCtx)
	require.NotNil(t, extracted)
	assert.Equal(t, "123", extracted[ClaimKeyUserID])
	assert.Equal(t, "admin", extracted[ClaimKeyRole])
}

func TestClaimsFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, ClaimsFromContext(ctx))
}

// --- AuthMiddleware tests ---

// headerTokenExtractor extracts the token from the named request header, for tests.
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
		// Verify the individually injected context values
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
	// Standard claims and token_type must not be injected
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

	// Generate an already expired access token
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

	// Using a refresh token as an access token must be rejected
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

// --- RFC 9110 §15.5.2 / RFC 6750 §3: 401 must carry WWW-Authenticate ---

// TestAuthMiddleware_UnauthorizedCarriesBearerChallenge is a regression test:
// every 401 path must carry a Bearer challenge in line with RFC 6750 §3.
//
// Historic defect: the 401 only set the status code and message with no
// WWW-Authenticate header, violating the RFC 9110 §15.5.2 MUST; clients could
// not tell which authentication scheme they were expected to use.
func TestAuthMiddleware_UnauthorizedCarriesBearerChallenge(t *testing.T) {
	j := newTestJWT(t)
	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))
	handler := mw(func(w http.ResponseWriter, r *http.Request) {})

	// Expired token
	expiredJWT, err := New(Config{
		Secret:            "test-secret-key",
		AccessTokenExpire: time.Millisecond,
	})
	require.NoError(t, err)
	expiredToken, err := expiredJWT.GenerateAccessToken(Claims{ClaimKeyUserID: "1"})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	// Invalid token (signed with the wrong secret)
	otherJWT, err := New(Config{Secret: "another-secret-key"})
	require.NoError(t, err)
	otherToken, err := otherJWT.GenerateAccessToken(Claims{ClaimKeyUserID: "1"})
	require.NoError(t, err)

	cases := []struct {
		name     string
		token    string
		wantCode string // error value from RFC 6750 §3
	}{
		{"missing token", "", middleware.BearerErrorInvalidRequest},
		{"expired token", expiredToken, middleware.BearerErrorInvalidToken},
		{"invalid token", otherToken, middleware.BearerErrorInvalidToken},
		{"malformed token", "not-a-jwt", middleware.BearerErrorInvalidToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.token != "" {
				req.Header.Set("X-Token", tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)

			challenge := rec.Header().Get(middleware.HeaderWWWAuthenticate)
			require.NotEmpty(t, challenge,
				"401 MUST carry WWW-Authenticate (RFC 9110 §15.5.2)")
			assert.Contains(t, challenge, "Bearer",
				"JWT uses the Bearer scheme (RFC 6750)")
			assert.Contains(t, challenge, `error="`+tc.wantCode+`"`,
				"error parameter must be one of the RFC 6750 §3 values")
		})
	}
}

// TestAuthMiddleware_ChallengeOnSuccess verifies that no challenge header is
// sent when authentication succeeds.
func TestAuthMiddleware_ChallengeOnSuccess(t *testing.T) {
	j := newTestJWT(t)
	token, err := j.GenerateAccessToken(Claims{ClaimKeyUserID: "1"})
	require.NoError(t, err)

	mw := j.AuthMiddleware(headerTokenExtractor("X-Token"))
	handler := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(middleware.HeaderWWWAuthenticate))
}
