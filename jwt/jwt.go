package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/chihqiang/infra-go/mapping"
	stdjwt "github.com/golang-jwt/jwt/v5"
)

// 错误定义。
var (
	// ErrInvalidToken 令牌无效。
	ErrInvalidToken = errors.New("jwt: invalid token")
	// ErrExpiredToken 令牌已过期。
	ErrExpiredToken = errors.New("jwt: token is expired")
	// ErrNotRefreshToken 不是刷新令牌。
	ErrNotRefreshToken = errors.New("jwt: token is not a refresh token")
	// ErrSecretEmpty 密钥为空。
	ErrSecretEmpty = errors.New("jwt: secret is empty")
	// ErrUnsupportedAlgorithm 不支持的签名算法。
	ErrUnsupportedAlgorithm = errors.New("jwt: unsupported algorithm")
)

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	// 用用户配置覆盖
	if cfg.Secret != "" {
		c.Secret = cfg.Secret
	}
	if cfg.Issuer != "" {
		c.Issuer = cfg.Issuer
	}
	if len(cfg.Audience) > 0 {
		c.Audience = cfg.Audience
	}
	if cfg.AccessTokenExpire > 0 {
		c.AccessTokenExpire = cfg.AccessTokenExpire
	}
	if cfg.RefreshTokenExpire > 0 {
		c.RefreshTokenExpire = cfg.RefreshTokenExpire
	}
	if cfg.Algorithm != "" {
		c.Algorithm = cfg.Algorithm
	}

	return c
}

// signingMethod 根据算法返回对应的 jwt.SigningMethod。
func signingMethod(alg Algorithm) (stdjwt.SigningMethod, error) {
	switch alg {
	case AlgorithmHS256:
		return stdjwt.SigningMethodHS256, nil
	case AlgorithmHS384:
		return stdjwt.SigningMethodHS384, nil
	case AlgorithmHS512:
		return stdjwt.SigningMethodHS512, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, alg)
	}
}

// --- JWT 面向对象封装 ---

// JWT 封装了 JWT 操作，配置只需初始化一次。
type JWT struct {
	config Config
	method stdjwt.SigningMethod
}

// New 创建 JWT 实例，配置只需传入一次。
func New(cfg Config) (*JWT, error) {
	c := fillDefault(cfg)

	if c.Secret == "" {
		return nil, ErrSecretEmpty
	}

	method, err := signingMethod(c.Algorithm)
	if err != nil {
		return nil, err
	}

	return &JWT{
		config: c,
		method: method,
	}, nil
}

// MustNew 创建 JWT 实例，出错时 panic。
// 适用于全局初始化场景。
func MustNew(cfg Config) *JWT {
	j, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return j
}

// Config 返回填充默认值后的配置。
func (j *JWT) Config() Config {
	return j.config
}

// --- 令牌生成 ---

// GenerateToken 生成单个令牌。
// claims 为自定义声明，会自动注入 iss、aud、iat、nbf、exp 标准字段。
// 如果 claims 中未设置 "token_type"，需调用方自行设置。
// expire 为令牌有效期。
func (j *JWT) GenerateToken(claims Claims, expire time.Duration) (string, error) {
	now := time.Now()

	// 注入标准声明
	claims[ClaimKeyIssuedAt] = now.Unix()
	claims[ClaimKeyNotBefore] = now.Unix()
	claims[ClaimKeyExpirationTime] = now.Add(expire).Unix()
	if j.config.Issuer != "" {
		claims[ClaimKeyIssuer] = j.config.Issuer
	}
	if len(j.config.Audience) > 0 {
		claims[ClaimKeyAudience] = j.config.Audience
	}

	token := stdjwt.NewWithClaims(j.method, claims)
	return token.SignedString([]byte(j.config.Secret))
}

// GenerateAccessToken 生成访问令牌，自动设置 token_type 为 access。
func (j *JWT) GenerateAccessToken(claims Claims) (string, error) {
	claims[ClaimKeyTokenType] = TokenTypeAccess
	return j.GenerateToken(claims, j.config.AccessTokenExpire)
}

// GenerateRefreshToken 生成刷新令牌，自动设置 token_type 为 refresh。
func (j *JWT) GenerateRefreshToken(claims Claims) (string, error) {
	claims[ClaimKeyTokenType] = TokenTypeRefresh
	return j.GenerateToken(claims, j.config.RefreshTokenExpire)
}

// GenerateTokenPair 生成访问令牌和刷新令牌对。
// claims 中的自定义字段会同时写入两个令牌。
func (j *JWT) GenerateTokenPair(claims Claims) (*TokenPair, error) {
	// 复制 claims，避免两个令牌共享底层数据
	accessClaims := copyClaims(claims)
	refreshClaims := copyClaims(claims)

	accessToken, err := j.GenerateAccessToken(accessClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := j.GenerateRefreshToken(refreshClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(j.config.AccessTokenExpire).Unix(),
	}, nil
}

// --- 令牌验证 ---

// ParseToken 解析并验证令牌，返回 claims。
func (j *JWT) ParseToken(tokenString string) (Claims, error) {
	claims := Claims{}

	token, err := stdjwt.ParseWithClaims(tokenString, claims, func(token *stdjwt.Token) (any, error) {
		// 验证签名算法
		if _, ok := token.Method.(*stdjwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method: %v", ErrInvalidToken, token.Header["alg"])
		}
		return []byte(j.config.Secret), nil
	})

	if err != nil {
		if errors.Is(err, stdjwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err.Error())
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ParseAccessToken 解析访问令牌，验证令牌类型为 access。
func (j *JWT) ParseAccessToken(tokenString string) (Claims, error) {
	claims, err := j.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}

	if tt, _ := claims[ClaimKeyTokenType].(string); tt != TokenTypeAccess {
		return nil, fmt.Errorf("%w: expected access token, got %s", ErrInvalidToken, tt)
	}

	return claims, nil
}

// ParseRefreshToken 解析刷新令牌，验证令牌类型为 refresh。
func (j *JWT) ParseRefreshToken(tokenString string) (Claims, error) {
	claims, err := j.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}

	if tt, _ := claims[ClaimKeyTokenType].(string); tt != TokenTypeRefresh {
		return nil, ErrNotRefreshToken
	}

	return claims, nil
}

// --- 令牌刷新 ---

// RefreshToken 使用刷新令牌生成新的令牌对。
// 旧的刷新令牌验证通过后，提取其中的自定义声明，生成全新的令牌对。
func (j *JWT) RefreshToken(refreshToken string) (*TokenPair, error) {
	claims, err := j.ParseRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// 移除标准声明和 token_type，只保留业务字段
	cleanClaims := extractBusinessClaims(claims)

	return j.GenerateTokenPair(cleanClaims)
}

// --- 内部辅助 ---

// copyClaims 深拷贝 MapClaims，避免共享底层数据。
func copyClaims(src Claims) Claims {
	dst := make(Claims, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// extractBusinessClaims 从 claims 中提取业务字段，
// 移除标准声明和 token_type。
func extractBusinessClaims(claims Claims) Claims {
	result := make(Claims)
	standardKeys := map[string]bool{
		ClaimKeyIssuer:         true,
		ClaimKeyAudience:       true,
		ClaimKeySubject:        true,
		ClaimKeyExpirationTime: true,
		ClaimKeyIssuedAt:       true,
		ClaimKeyNotBefore:      true,
		ClaimKeyTokenType:      true,
		ClaimKeyJWTID:          true,
	}
	for k, v := range claims {
		if !standardKeys[k] {
			result[k] = v
		}
	}
	return result
}
