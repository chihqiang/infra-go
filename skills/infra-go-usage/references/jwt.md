# jwt

A JWT wrapper package built on [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt), designed in an object-oriented way: the configuration is initialized once, and `Claims` (an alias for golang-jwt's `MapClaims`) supports freely extending the claim fields.

## Features

- **Object-oriented**: a `JWT` instance encapsulates the configuration, so parameters don't have to be passed every call
- **Free-form claims**: `jwt.Claims` is a type alias for golang-jwt's `MapClaims` (`map[string]any`), so any claim field can be added freely
- **Dual-token mode**: access token (short-lived) + refresh token (long-lived), generating token pairs automatically
- **Multiple algorithms**: HS256 / HS384 / HS512
- **Token refresh**: use a refresh token to generate a brand-new token pair
- **Type validation**: distinguishes access tokens from refresh tokens to prevent mixing them up
- **Configuration-driven**: Config defines defaults with `default` struct tags, following the conf standard
- **Unified errors**: semantic errors (`ErrInvalidToken`, `ErrExpiredToken`, etc.) that are easy for callers to handle
- **Constant management**: both standard and business claim keys are constants, avoiding hard-coded strings
- **HTTP auth middleware**: `AuthMiddleware` validates the token and injects the business claims into the context; on the httpx side you can register it conveniently with `httpx.WithJWT` (see [httpx](./httpx.md))

## Dependencies

`jwt` **does not depend on the httpx main package** (it only relies on the `httpx/middleware` subpackage for error output), so the httpx main package can reference jwt in reverse to expose `httpx.WithJWT` without a circular dependency. Authentication failures are emitted through the unified error mechanism of `httpx/middleware`: when the application imports the httpx main package they become its unified `Response[T]` JSON (carrying `request_id`), otherwise they degrade to plain-text `http.Error`.

## Installation

```bash
go get github.com/chihqiang/infra-go/jwt
```

## Quick start

```go
package main

import (
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/jwt"
)

func main() {
    // initialization only needs to happen once
    j := jwt.MustNew(jwt.Config{
        Secret:             "my-secret-key",
        Issuer:             "my-app",
        AccessTokenExpire:  2 * time.Hour,
        RefreshTokenExpire: 7 * 24 * time.Hour,
        Algorithm:          jwt.AlgorithmHS256,
    })

    // generate a token pair
    pair, err := j.GenerateTokenPair(jwt.Claims{
        jwt.ClaimKeyUserID:   "user-123",
        jwt.ClaimKeyUsername: "alice",
        jwt.ClaimKeyRole:     "admin",
    })
    if err != nil {
        panic(err)
    }

    // validate the access token
    claims, err := j.ParseAccessToken(pair.AccessToken)
    if err != nil {
        panic(err)
    }
    fmt.Printf("UserID: %v\n", claims[jwt.ClaimKeyUserID])
}
```

## API

### Creating an instance

```go
j, err := jwt.New(jwt.Config{Secret: "my-secret-key", Issuer: "my-app", ...}) // returns an error
j := jwt.MustNew(jwt.Config{Secret: "my-secret-key"})                         // panics on error
```

### Token generation

```go
token, err := j.GenerateAccessToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})   // sets token_type=access automatically
token, err := j.GenerateRefreshToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})  // sets token_type=refresh automatically
pair, err := j.GenerateTokenPair(jwt.Claims{...})                                  // generates access + refresh together
token, err := j.GenerateToken(jwt.Claims{jwt.ClaimKeyUserID: "123"}, 30*time.Minute) // custom expiry
```

### Token validation

```go
claims, err := j.ParseToken(tokenString)          // parse (without validating the type)
claims, err := j.ParseAccessToken(tokenString)    // validate an access token
claims, err := j.ParseRefreshToken(tokenString)   // validate a refresh token
```

### Token refresh

```go
newPair, err := j.RefreshToken(oldRefreshToken) // generate a new token pair from a refresh token
```

### Claims and ClaimKey

`jwt.Claims` is a type alias for golang-jwt's `MapClaims` (`map[string]any`) and can be extended freely:

```go
claims := jwt.Claims{
    jwt.ClaimKeyUserID: "user-123",
    jwt.ClaimKeyRole:   "admin",
    "meta":             map[string]any{"department": "engineering"}, // custom key
}
userID, _ := claims[jwt.ClaimKeyUserID].(string) // a type assertion is needed when reading
```

Predefined key constants:

| Constant | Value | Description |
|------|----|------|
| `ClaimKeyIssuer` | `"iss"` | Issuer |
| `ClaimKeySubject` | `"sub"` | Subject |
| `ClaimKeyAudience` | `"aud"` | Audience |
| `ClaimKeyExpirationTime` | `"exp"` | Expiration time |
| `ClaimKeyNotBefore` | `"nbf"` | Not-before time |
| `ClaimKeyIssuedAt` | `"iat"` | Issued-at time |
| `ClaimKeyJWTID` | `"jti"` | Unique JWT identifier |
| `ClaimKeyTokenType` | `"token_type"` | Token type |
| `ClaimKeyUserID` | `"user_id"` | User ID |
| `ClaimKeyUsername` | `"username"` | Username |
| `ClaimKeyRole` | `"role"` | Role |
| `ClaimKeyPermissions` | `"permissions"` | Permission list |
| `ClaimKeyScopes` | `"scopes"` | Scope list |

`TokenPair`:

```go
type TokenPair struct {
    AccessToken  string // access token
    RefreshToken string // refresh token
    ExpiresAt    int64  // access token expiry timestamp (seconds)
}
```

## Configuration

| Field | Type | Default | Description |
|------|------|--------|------|
| `Secret` | `string` | `""` | HMAC signing key (required) |
| `Issuer` | `string` | `""` | Issuer identifier |
| `Audience` | `[]string` | `nil` | Audience list |
| `AccessTokenExpire` | `time.Duration` | `2h` | Access token lifetime |
| `RefreshTokenExpire` | `time.Duration` | `168h` | Refresh token lifetime |
| `Algorithm` | `Algorithm` | `HS256` | Signing algorithm |

Signing algorithms: `AlgorithmHS256`("HS256") / `AlgorithmHS384`("HS384") / `AlgorithmHS512`("HS512").

## Error handling

```go
claims, err := j.ParseAccessToken(tokenString)
switch {
case err == nil:
    // success
case errors.Is(err, jwt.ErrExpiredToken):
    // expired, needs refreshing
case errors.Is(err, jwt.ErrInvalidToken):
    // invalid (signature/format/type mismatch)
case errors.Is(err, jwt.ErrNotRefreshToken):
    // not a refresh token
}
```

| Error | Description |
|------|------|
| `ErrInvalidToken` | Token is invalid (bad signature, malformed, type mismatch, etc.) |
| `ErrExpiredToken` | Token has expired |
| `ErrNotRefreshToken` | Token is not a refresh token |
| `ErrSecretEmpty` | The secret is empty |
| `ErrUnsupportedAlgorithm` | Unsupported signing algorithm |

## Typical integration: HTTP authentication

### Option 1: httpx.WithJWT (recommended, httpx services)

```go
j := jwt.MustNew(jwt.Config{Secret: cfg.JWTSecret})

server.Use(httpx.WithJWT(j, func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
```

### Option 2: use jwt.AuthMiddleware directly

`AuthMiddleware(getToken)` returns `func(http.HandlerFunc) http.HandlerFunc` (that is, an `httpx.Middleware`), so it can be passed to `server.Use` directly:

```go
server.Use(j.AuthMiddleware(func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
```

The two are equivalent; the caller decides where `getToken` reads the token from (Header/Cookie/Query).

### Reading claims downstream

Once authentication succeeds the middleware injects the **business claims** (excluding the standard claims and `token_type`) into the context:

```go
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
    claims := jwt.ClaimsFromContext(r.Context())
    userID, _ := claims[jwt.ClaimKeyUserID].(string)
    // ...
}
```

> A failed authentication returns 401 and, per RFC 9110 §15.5.2 (MUST), carries a `WWW-Authenticate` challenge;
> the `error` parameter comes from RFC 6750 §3:
>
> | Scenario | `error` value |
> |------|-------------|
> | No token provided | `invalid_request` |
> | Token expired / invalid / malformed | `invalid_token` |
>
> ```http
> HTTP/1.1 401 Unauthorized
> WWW-Authenticate: Bearer error="invalid_token"
>
> {"code":401,"msg":"token expired","request_id":"..."}
> ```
>
> When the httpx main package is imported the error body is unified JSON (as above); otherwise it degrades to plain-text `http.Error`.
> Clients can use this to decide between redirecting to login (`invalid_request`) and refreshing the token (`invalid_token`).

## Complete example

```go
package main

import (
    "errors"
    "fmt"
    "net/http"
    "time"

    "github.com/chihqiang/infra-go/jwt"
    "github.com/chihqiang/infra-go/logger"
)

func main() {
    j := jwt.MustNew(jwt.Config{
        Secret:             "super-secret-key",
        Issuer:             "my-app",
        Audience:           []string{"web", "app"},
        AccessTokenExpire:  2 * time.Hour,
        RefreshTokenExpire: 7 * 24 * time.Hour,
        Algorithm:          jwt.AlgorithmHS256,
    })

    // login: generate a token pair
    pair, err := j.GenerateTokenPair(jwt.Claims{
        jwt.ClaimKeyUserID: "user-001", jwt.ClaimKeyRole: "admin",
    })
    if err != nil {
        logger.Fatal("failed to generate token pair", logger.Err(err))
    }
    fmt.Printf("Access: %s...\n", pair.AccessToken[:30])

    // validate the access token
    claims, err := j.ParseAccessToken(pair.AccessToken)
    if err != nil {
        logger.Fatal("failed to parse access token", logger.Err(err))
    }
    fmt.Printf("UserID: %v\n", claims[jwt.ClaimKeyUserID])

    // refresh the token
    newPair, err := j.RefreshToken(pair.RefreshToken)
    if err != nil {
        if errors.Is(err, jwt.ErrExpiredToken) {
            fmt.Println("refresh token expired, need re-login")
        } else {
            logger.Fatal("failed to refresh token", logger.Err(err))
        }
    }
    fmt.Printf("New Access: %s...\n", newPair.AccessToken[:30])

    // use as HTTP middleware (either httpx.WithJWT or j.AuthMiddleware works)
    _ = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}
```
