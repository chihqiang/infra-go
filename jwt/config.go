package jwt

import "time"

// Algorithm JWT 签名算法类型。
type Algorithm string

const (
	// AlgorithmHS256 HMAC-SHA256。
	AlgorithmHS256 Algorithm = "HS256"
	// AlgorithmHS384 HMAC-SHA384。
	AlgorithmHS384 Algorithm = "HS384"
	// AlgorithmHS512 HMAC-SHA512。
	AlgorithmHS512 Algorithm = "HS512"
)

// 令牌类型。
const (
	// TokenTypeAccess 访问令牌。
	TokenTypeAccess = "access"
	// TokenTypeRefresh 刷新令牌。
	TokenTypeRefresh = "refresh"
)

// Config JWT 配置。
type Config struct {
	// Secret HMAC 签名密钥（HS256/HS384/HS512）。
	Secret string `json:",optional"`
	// Issuer 签发者，例如 "my-app"。
	Issuer string `json:",optional"`
	// Audience 签发受众，例如 ["web", "app"]。
	Audience []string `json:",optional"`
	// AccessTokenExpire 访问令牌过期时间，默认 2 小时。
	AccessTokenExpire time.Duration `json:",default=2h"`
	// RefreshTokenExpire 刷新令牌过期时间，默认 168 小时（7 天）。
	RefreshTokenExpire time.Duration `json:",default=168h"`
	// Algorithm 签名算法，默认 HS256。
	Algorithm Algorithm `json:",default=HS256"`
}

// TokenPair 包含访问令牌和刷新令牌。
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64 // 访问令牌过期时间戳（秒）
}
