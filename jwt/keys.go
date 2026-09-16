package jwt

// Standard claim keys (RFC 7519).
const (
	// ClaimKeyIssuer is the issuer claim.
	ClaimKeyIssuer = "iss"
	// ClaimKeySubject is the subject claim.
	ClaimKeySubject = "sub"
	// ClaimKeyAudience is the audience claim.
	ClaimKeyAudience = "aud"
	// ClaimKeyExpirationTime is the expiration time claim.
	ClaimKeyExpirationTime = "exp"
	// ClaimKeyNotBefore is the not-before claim.
	ClaimKeyNotBefore = "nbf"
	// ClaimKeyIssuedAt is the issued-at claim.
	ClaimKeyIssuedAt = "iat"
	// ClaimKeyJWTID is the unique JWT identifier claim.
	ClaimKeyJWTID = "jti"
)

// Custom claim keys.
const (
	// ClaimKeyTokenType is the token type: access / refresh.
	ClaimKeyTokenType = "token_type"
)

// Common business claim keys, usable directly when generating tokens.
const (
	// ClaimKeyUserID is the user ID claim.
	ClaimKeyUserID = "user_id"
	// ClaimKeyUsername is the username claim.
	ClaimKeyUsername = "username"
	// ClaimKeyRole is the role claim.
	ClaimKeyRole = "role"
	// ClaimKeyPermissions is the permission list claim.
	ClaimKeyPermissions = "permissions"
	// ClaimKeyScopes is the scope list claim.
	ClaimKeyScopes = "scopes"
)
