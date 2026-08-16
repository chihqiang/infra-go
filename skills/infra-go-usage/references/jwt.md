# jwt

基于 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) 的 JWT 封装包，面向对象设计，配置只需初始化一次，使用 `MapClaims` 支持自由扩展声明字段。

## 特性

- **面向对象**：`JWT` 实例封装配置，无需每次传参
- **MapClaims**：使用 `jwt.MapClaims` 别名，自由扩展任意声明字段
- **双令牌模式**：访问令牌（短期）+ 刷新令牌（长期），自动生成令牌对
- **多算法支持**：HS256、HS384、HS512
- **令牌刷新**：使用刷新令牌生成全新的令牌对
- **类型验证**：区分访问令牌和刷新令牌，防止混用
- **配置驱动**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **统一错误**：提供语义化错误（`ErrInvalidToken`、`ErrExpiredToken` 等），便于上层处理
- **常量管理**：所有标准声明和常用业务声明的 key 均定义为常量，避免硬编码字符串
- **HTTP 中间件**：提供 `AuthMiddleware`，验证令牌后将 claims 注入请求 context

## 安装

```bash
go get github.com/chihqiang/infra-go/jwt
```

## 快速开始

```go
package main

import (
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/jwt"
)

func main() {
    // 初始化只需一次
    j := jwt.MustNew(jwt.Config{
        Secret:             "my-secret-key",
        Issuer:             "my-app",
        AccessTokenExpire:  2 * time.Hour,
        RefreshTokenExpire: 7 * 24 * time.Hour,
        Algorithm:          jwt.AlgorithmHS256,
    })

    // 生成令牌对
    pair, err := j.GenerateTokenPair(jwt.Claims{
        jwt.ClaimKeyUserID:  "user-123",
        jwt.ClaimKeyUsername: "alice",
        jwt.ClaimKeyRole:     "admin",
    })
    if err != nil {
        panic(err)
    }

    // 验证访问令牌
    claims, err := j.ParseAccessToken(pair.AccessToken)
    if err != nil {
        panic(err)
    }
    fmt.Printf("UserID: %v\n", claims[jwt.ClaimKeyUserID])
}
```

## API

### 创建实例

```go
// 创建实例（返回 error）
j, err := jwt.New(jwt.Config{
    Secret:             "my-secret-key",
    Issuer:             "my-app",
    AccessTokenExpire:  2 * time.Hour,
    RefreshTokenExpire: 7 * 24 * time.Hour,
    Algorithm:          jwt.AlgorithmHS256,
})

// 创建实例（出错 panic，适合全局初始化）
j := jwt.MustNew(jwt.Config{Secret: "my-secret-key"})
```

### 令牌生成

```go
// 生成访问令牌（自动设置 token_type=access）
token, err := j.GenerateAccessToken(jwt.Claims{
    jwt.ClaimKeyUserID:  "user-123",
    jwt.ClaimKeyUsername: "alice",
})

// 生成刷新令牌（自动设置 token_type=refresh）
token, err := j.GenerateRefreshToken(jwt.Claims{
    jwt.ClaimKeyUserID: "user-123",
})

// 生成令牌对（同时生成 access + refresh）
pair, err := j.GenerateTokenPair(jwt.Claims{
    jwt.ClaimKeyUserID:  "user-123",
    jwt.ClaimKeyUsername: "alice",
    jwt.ClaimKeyRole:     "admin",
})

// 生成自定义过期时间的令牌
token, err := j.GenerateToken(jwt.Claims{jwt.ClaimKeyUserID: "123"}, 30*time.Minute)
```

### 令牌验证

```go
// 解析令牌（不验证类型），返回 claims
claims, err := j.ParseToken(tokenString)

// 验证访问令牌，返回 claims
claims, err := j.ParseAccessToken(tokenString)

// 验证刷新令牌，返回 claims
claims, err := j.ParseRefreshToken(tokenString)
```

### 令牌刷新

```go
// 使用刷新令牌生成新的令牌对
newPair, err := j.RefreshToken(oldRefreshToken)
```

### Claims 类型

`jwt.Claims` 是 `jwt.MapClaims` 的别名，即 `map[string]any`，可自由扩展：

```go
claims := jwt.Claims{
    jwt.ClaimKeyUserID:  "user-123",           // 字符串
    jwt.ClaimKeyUsername: "alice",              // 字符串
    jwt.ClaimKeyRole:     "admin",              // 字符串
    jwt.ClaimKeyScopes:   []string{"read"},     // 切片
    "meta":               map[string]any{       // 嵌套对象（可自定义 key）
        "department": "engineering",
    },
}

// 读取时需类型断言
claims, err := j.ParseAccessToken(tokenString)
userID, _ := claims[jwt.ClaimKeyUserID].(string)
```

### ClaimKey 常量

为避免硬编码字符串，包中预定义了标准声明和常用业务声明的 key 常量：

| 常量 | 值 | 说明 |
| ------ | ---- | ------ |
| `ClaimKeyIssuer` | `"iss"` | 签发者 |
| `ClaimKeySubject` | `"sub"` | 主题 |
| `ClaimKeyAudience` | `"aud"` | 受众 |
| `ClaimKeyExpirationTime` | `"exp"` | 过期时间 |
| `ClaimKeyNotBefore` | `"nbf"` | 生效时间 |
| `ClaimKeyIssuedAt` | `"iat"` | 签发时间 |
| `ClaimKeyJWTID` | `"jti"` | JWT 唯一标识 |
| `ClaimKeyTokenType` | `"token_type"` | 令牌类型 |
| `ClaimKeyUserID` | `"user_id"` | 用户 ID |
| `ClaimKeyUsername` | `"username"` | 用户名 |
| `ClaimKeyRole` | `"role"` | 角色 |
| `ClaimKeyPermissions` | `"permissions"` | 权限列表 |
| `ClaimKeyScopes` | `"scopes"` | 作用域列表 |

### TokenPair 结构

```go
type TokenPair struct {
    AccessToken  string // 访问令牌
    RefreshToken string // 刷新令牌
    ExpiresAt    int64  // 访问令牌过期时间戳（秒）
}
```

## 配置

### 配置项说明

| 字段 | 类型 | 默认值 | 说明 |
| ------ | ------ | -------- | ------ |
| `Secret` | `string` | `""` | HMAC 签名密钥 |
| `Issuer` | `string` | `""` | 签发者标识 |
| `Audience` | `[]string` | `nil` | 受众列表 |
| `AccessTokenExpire` | `time.Duration` | `2h` | 访问令牌有效期 |
| `RefreshTokenExpire` | `time.Duration` | `168h` | 刷新令牌有效期 |
| `Algorithm` | `Algorithm` | `HS256` | 签名算法 |

### 签名算法

| 常量 | 值 | 说明 |
| ------ | ---- | ------ |
| `AlgorithmHS256` | `"HS256"` | HMAC-SHA256 |
| `AlgorithmHS384` | `"HS384"` | HMAC-SHA384 |
| `AlgorithmHS512` | `"HS512"` | HMAC-SHA512 |

## 错误处理

```go
claims, err := j.ParseAccessToken(tokenString)
switch {
case err == nil:
    // 验证成功
case errors.Is(err, jwt.ErrExpiredToken):
    // 令牌已过期，需要刷新
case errors.Is(err, jwt.ErrInvalidToken):
    // 令牌无效（签名错误、格式错误、类型不匹配等）
case errors.Is(err, jwt.ErrNotRefreshToken):
    // 不是刷新令牌
}
```

| 错误 | 说明 |
| ------ | ------ |
| `ErrInvalidToken` | 令牌无效（签名错误、格式错误、类型不匹配等） |
| `ErrExpiredToken` | 令牌已过期 |
| `ErrNotRefreshToken` | 令牌不是刷新令牌 |
| `ErrSecretEmpty` | 密钥为空 |
| `ErrUnsupportedAlgorithm` | 不支持的签名算法 |

## 完整示例

```go
package main

import (
    "errors"
    "fmt"
    "log"
    "time"

    "github.com/chihqiang/infra-go/jwt"
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

    // === 登录：生成令牌对 ===
    pair, err := j.GenerateTokenPair(jwt.Claims{
        jwt.ClaimKeyUserID:  "user-001",
        jwt.ClaimKeyUsername: "alice",
        jwt.ClaimKeyRole:     "admin",
        jwt.ClaimKeyScopes:   []string{"read", "write"},
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("=== Login ===")
    fmt.Printf("Access Token:  %s...\n", pair.AccessToken[:30])
    fmt.Printf("Refresh Token: %s...\n", pair.RefreshToken[:30])

    // === 验证访问令牌 ===
    fmt.Println("\n=== Verify Access Token ===")
    claims, err := j.ParseAccessToken(pair.AccessToken)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("UserID:   %v\n", claims[jwt.ClaimKeyUserID])
    fmt.Printf("Username: %v\n", claims[jwt.ClaimKeyUsername])

    // === 刷新令牌 ===
    fmt.Println("\n=== Refresh Token ===")
    newPair, err := j.RefreshToken(pair.RefreshToken)
    if err != nil {
        if errors.Is(err, jwt.ErrExpiredToken) {
            fmt.Println("refresh token expired, need re-login")
        } else {
            log.Fatal(err)
        }
    }
    fmt.Printf("New Access Token: %s...\n", newPair.AccessToken[:30])
}
```

## 典型集成

### HTTP 中间件

```go
var j *jwt.JWT // 全局初始化

// AuthMiddleware 验证令牌后将 claims 注入请求 context。
// getToken 由调用方提供，从请求中提取 token（如从 Authorization 头）。
func AuthMiddleware() func(http.HandlerFunc) http.HandlerFunc {
    return j.AuthMiddleware(func(r *http.Request) string {
        auth := r.Header.Get("Authorization")
        return strings.TrimPrefix(auth, "Bearer ")
    })
}

// 下游 handler 从 context 获取 claims
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
    claims := jwt.ClaimsFromContext(r.Context())
    userID, _ := claims[jwt.ClaimKeyUserID].(string)
    // ...
}
```

### 刷新令牌端点

```go
func RefreshHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            RefreshToken string `json:"refresh_token"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "bad request", http.StatusBadRequest)
            return
        }

        pair, err := j.RefreshToken(req.RefreshToken)
        if err != nil {
            http.Error(w, "invalid refresh token", http.StatusUnauthorized)
            return
        }

        json.NewEncoder(w).Encode(map[string]any{
            "access_token":  pair.AccessToken,
            "refresh_token": pair.RefreshToken,
            "expires_at":    pair.ExpiresAt,
        })
    }
}
```
