package jwt

import (
	"context"
	"errors"
	"net/http"

	"github.com/chihqiang/infra-go/httpx/middleware"
	stdjwt "github.com/golang-jwt/jwt/v5"
)

// 认证失败响应消息。
const (
	msgTokenMissing = "token is missing"
	msgTokenExpired = "token expired"
	msgInvalidToken = "invalid token"
)

// claimCtxKey 用于在 context 中存储单个 claim 值的键类型。
// 用未导出类型包装 claim key，避免与其他包的 context key 冲突。
type claimCtxKey string

// Claims 是 jwt.MapClaims 的别名，方便外部使用。
// 使用 MapClaims 可以自由扩展任意字段，无需固定结构体。
type Claims = stdjwt.MapClaims

// contextKey 用于在 context 中存储 claims 的键类型。
type contextKey struct{}

// claimsKey 是 context 中存储 claims 的键。
var claimsKey = contextKey{}

// WithClaims 将 claims 写入 context，返回新的 context。
// 后续可通过 ClaimsFromContext 提取。
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext 从 context 中提取 claims。
// 如果 context 中没有 claims，返回 nil。
func ClaimsFromContext(ctx context.Context) Claims {
	claims, _ := ctx.Value(claimsKey).(Claims)
	return claims
}

// bearerChallenge 返回 401 响应的 WWW-Authenticate 质询。
//
// RFC 9110 §15.5.2 要求 401 MUST 携带 WWW-Authenticate；
// JWT 采用 Bearer 方案，因此按 RFC 6750 §3 产出质询，
// error 参数区分"缺凭证"与"凭证无效"，便于客户端决定是跳登录还是刷新令牌。
func bearerChallenge(errCode string) middleware.Challenge {
	return middleware.Challenge{Scheme: "Bearer", Error: errCode}
}

// AuthMiddleware 返回 JWT 认证中间件。
//
// getToken 由调用方提供，从请求中提取 token（如从 Header/Cookie/Query），
// 中间件只负责解析验证和注入 claims，不关心 token 来源。
//
// 验证失败返回 401 Unauthorized，并携带符合 RFC 6750 §3 的
// `WWW-Authenticate: Bearer error="..."` 质询；
// 错误响应经 httpx/middleware 的统一错误机制输出
// （import httpx 主包时为其统一 JSON 响应，否则为 http.Error 纯文本）。
//
// 返回类型为 func(http.HandlerFunc) http.HandlerFunc，兼容 httpx.Middleware，
// 可直接 server.Use 注册；也可通过 httpx.WithJWT 便捷注册。
func (j *JWT) AuthMiddleware(getToken func(*http.Request) string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := getToken(r)
			if token == "" {
				middleware.WriteUnauthorized(r.Context(), w,
					bearerChallenge(middleware.BearerErrorInvalidRequest), msgTokenMissing)
				return
			}

			claims, err := j.ParseAccessToken(token)
			if err != nil {
				if errors.Is(err, ErrExpiredToken) {
					// 过期令牌仍是"无效令牌"，RFC 6750 无独立取值；
					// 保留可读消息以便客户端区分（提示刷新而非重新登录）。
					middleware.WriteUnauthorized(r.Context(), w,
						bearerChallenge(middleware.BearerErrorInvalidToken), msgTokenExpired)
				} else {
					middleware.WriteUnauthorized(r.Context(), w,
						bearerChallenge(middleware.BearerErrorInvalidToken), msgInvalidToken)
				}
				return
			}

			// 将业务声明（排除标准声明和 token_type）逐个注入 context
			business := extractBusinessClaims(claims)
			ctx := r.Context()
			for k, v := range business {
				ctx = context.WithValue(ctx, claimCtxKey(k), v)
			}
			ctx = WithClaims(ctx, business)
			next(w, r.WithContext(ctx))
		}
	}
}
