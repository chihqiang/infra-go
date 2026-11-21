package token

import (
	"context"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// standardClaims are the registered JWT claims: they describe the token itself
// rather than the caller, so WithClaims does not copy them into the context.
var standardClaims = map[string]struct{}{
	"aud": {},
	"exp": {},
	"iat": {},
	"iss": {},
	"jti": {},
	"nbf": {},
	"sub": {},
}

type claimKey struct{ name string }

// WithClaims stores every custom claim in ctx keyed by its claim name, so
// handlers read them with Claim. Standard JWT claims are skipped. A nil ctx is
// replaced by context.Background() so a standalone handler cannot panic on it.
func WithClaims(ctx context.Context, claims jwtlib.MapClaims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	for name, value := range claims {
		if _, standard := standardClaims[name]; standard {
			continue
		}
		ctx = context.WithValue(ctx, claimKey{name: name}, value)
	}
	return ctx
}

// Claim returns the custom claim stored in ctx by WithClaims. A nil ctx or an
// absent claim reports false.
func Claim(ctx context.Context, name string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	value := ctx.Value(claimKey{name: name})
	return value, value != nil
}
