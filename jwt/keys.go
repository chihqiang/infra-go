package jwt

// 标准声明键（RFC 7519）。
const (
	// ClaimKeyIssuer 签发者。
	ClaimKeyIssuer = "iss"
	// ClaimKeySubject 主题。
	ClaimKeySubject = "sub"
	// ClaimKeyAudience 受众。
	ClaimKeyAudience = "aud"
	// ClaimKeyExpirationTime 过期时间。
	ClaimKeyExpirationTime = "exp"
	// ClaimKeyNotBefore 生效时间。
	ClaimKeyNotBefore = "nbf"
	// ClaimKeyIssuedAt 签发时间。
	ClaimKeyIssuedAt = "iat"
	// ClaimKeyJWTID JWT 唯一标识。
	ClaimKeyJWTID = "jti"
)

// 自定义声明键。
const (
	// ClaimKeyTokenType 令牌类型：access / refresh。
	ClaimKeyTokenType = "token_type"
)

// 常用业务声明键，可在生成令牌时直接使用。
const (
	// ClaimKeyUserID 用户 ID。
	ClaimKeyUserID = "user_id"
	// ClaimKeyUsername 用户名。
	ClaimKeyUsername = "username"
	// ClaimKeyRole 角色。
	ClaimKeyRole = "role"
	// ClaimKeyPermissions 权限列表。
	ClaimKeyPermissions = "permissions"
	// ClaimKeyScopes 作用域列表。
	ClaimKeyScopes = "scopes"
)
