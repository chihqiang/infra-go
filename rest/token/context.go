package token

import "context"

type claimsContextKey struct{}

// WithClaims stores verified claims in ctx. A nil ctx is replaced by
// context.Background() so a standalone handler cannot panic on it.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFrom returns the claims stored in ctx by WithClaims (the auth handler
// uses it). A nil ctx carries no claims.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	if ctx == nil {
		return nil, false
	}
	claims, ok := ctx.Value(claimsContextKey{}).(*Claims)
	return claims, ok
}
