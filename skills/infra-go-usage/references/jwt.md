# jwt

基于 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) 的 JWT 封装包，面向对象设计，配置只初始化一次，用 `MapClaims` 支持自由扩展声明字段。

## 特性

- **面向对象**：`JWT` 实例封装配置，无需每次传参
- **MapClaims**：使用 `jwt.MapClaims` 别名，自由扩展任意声明字段
- **双令牌模式**：访问令牌（短期）+ 刷新令牌（长期），自动生成令牌对
- **多算法支持**：HS256 / HS384 / HS512
- **令牌刷新**：用刷新令牌生成全新令牌对
- **类型验证**：区分访问令牌与刷新令牌，防止混用
- **配置驱动**：Config 用 `default` 结构体标签定义默认值，遵循 conf 标准
- **统一错误**：语义化错误（`ErrInvalidToken`、`ErrExpiredToken` 等），便于上层处理
- **常量管理**：标准/业务声明的 key 均为常量，避免硬编码字符串
- **HTTP 认证中间件**：`AuthMiddleware` 验证令牌后把业务 claims 注入 context；httpx 侧可用 `httpx.WithJWT` 便捷注册（见 [httpx](./httpx.md)）

## 依赖关系

`jwt` **不依赖 httpx 主包**（仅依赖 `httpx/middleware` 子包输出错误），因此 httpx 主包可反向引用 jwt 暴露 `httpx.WithJWT`，无循环依赖。认证失败经 `httpx/middleware` 的统一错误机制输出：应用 import 了 httpx 主包时为其统一 `Response[T]` JSON（携带 `request_id`），否则退化为 `http.Error` 纯文本。

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
        jwt.ClaimKeyUserID:   "user-123",
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
j, err := jwt.New(jwt.Config{Secret: "my-secret-key", Issuer: "my-app", ...}) // 返回 error
j := jwt.MustNew(jwt.Config{Secret: "my-secret-key"})                         // 出错 panic
```

### 令牌生成

```go
token, err := j.GenerateAccessToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})   // 自动 token_type=access
token, err := j.GenerateRefreshToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})  // 自动 token_type=refresh
pair, err := j.GenerateTokenPair(jwt.Claims{...})                                  // access + refresh 同时生成
token, err := j.GenerateToken(jwt.Claims{jwt.ClaimKeyUserID: "123"}, 30*time.Minute) // 自定义过期时间
```

### 令牌验证

```go
claims, err := j.ParseToken(tokenString)          // 解析（不验证类型）
claims, err := j.ParseAccessToken(tokenString)    // 验证访问令牌
claims, err := j.ParseRefreshToken(tokenString)   // 验证刷新令牌
```

### 令牌刷新

```go
newPair, err := j.RefreshToken(oldRefreshToken) // 用刷新令牌生成新令牌对
```

### Claims 与 ClaimKey

`jwt.Claims` 是 `jwt.MapClaims` 别名（`map[string]any`），可自由扩展：

```go
claims := jwt.Claims{
    jwt.ClaimKeyUserID: "user-123",
    jwt.ClaimKeyRole:   "admin",
    "meta":             map[string]any{"department": "engineering"}, // 自定义 key
}
userID, _ := claims[jwt.ClaimKeyUserID].(string) // 读取时需类型断言
```

预定义 key 常量：

| 常量 | 值 | 说明 |
|------|----|------|
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

`TokenPair`：

```go
type TokenPair struct {
    AccessToken  string // 访问令牌
    RefreshToken string // 刷新令牌
    ExpiresAt    int64  // 访问令牌过期时间戳（秒）
}
```

## 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Secret` | `string` | `""` | HMAC 签名密钥（必填） |
| `Issuer` | `string` | `""` | 签发者标识 |
| `Audience` | `[]string` | `nil` | 受众列表 |
| `AccessTokenExpire` | `time.Duration` | `2h` | 访问令牌有效期 |
| `RefreshTokenExpire` | `time.Duration` | `168h` | 刷新令牌有效期 |
| `Algorithm` | `Algorithm` | `HS256` | 签名算法 |

签名算法：`AlgorithmHS256`("HS256") / `AlgorithmHS384`("HS384") / `AlgorithmHS512`("HS512")。

## 错误处理

```go
claims, err := j.ParseAccessToken(tokenString)
switch {
case err == nil:
    // 成功
case errors.Is(err, jwt.ErrExpiredToken):
    // 过期，需要刷新
case errors.Is(err, jwt.ErrInvalidToken):
    // 无效（签名/格式/类型不匹配）
case errors.Is(err, jwt.ErrNotRefreshToken):
    // 不是刷新令牌
}
```

| 错误 | 说明 |
|------|------|
| `ErrInvalidToken` | 令牌无效（签名错误、格式错误、类型不匹配等） |
| `ErrExpiredToken` | 令牌已过期 |
| `ErrNotRefreshToken` | 令牌不是刷新令牌 |
| `ErrSecretEmpty` | 密钥为空 |
| `ErrUnsupportedAlgorithm` | 不支持的签名算法 |

## 典型集成：HTTP 认证

### 方式一：httpx.WithJWT（推荐，httpx 服务）

```go
j := jwt.MustNew(jwt.Config{Secret: cfg.JWTSecret})

server.Use(httpx.WithJWT(j, func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
```

### 方式二：直接使用 jwt.AuthMiddleware

`AuthMiddleware(getToken)` 返回 `func(http.HandlerFunc) http.HandlerFunc`（即 `httpx.Middleware`），可直接 `server.Use`：

```go
server.Use(j.AuthMiddleware(func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
```

两者等价；`getToken` 由调用方决定 token 来源（Header/Cookie/Query）。

### 下游读取 claims

认证通过后中间件把**业务声明**（排除标准声明与 `token_type`）注入 context：

```go
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
    claims := jwt.ClaimsFromContext(r.Context())
    userID, _ := claims[jwt.ClaimKeyUserID].(string)
    // ...
}
```

> 认证失败返回 401。经 httpx 主包 import 时错误为统一 JSON：
> ```json
> {"code":401,"msg":"token is missing","request_id":"..."}
> ```
> 未 import httpx 主包时退化为 `http.Error` 纯文本。

## 完整示例

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

    // 登录：生成令牌对
    pair, err := j.GenerateTokenPair(jwt.Claims{
        jwt.ClaimKeyUserID: "user-001", jwt.ClaimKeyRole: "admin",
    })
    if err != nil {
        logger.Fatal("failed to generate token pair", logger.Err(err))
    }
    fmt.Printf("Access: %s...\n", pair.AccessToken[:30])

    // 验证访问令牌
    claims, err := j.ParseAccessToken(pair.AccessToken)
    if err != nil {
        logger.Fatal("failed to parse access token", logger.Err(err))
    }
    fmt.Printf("UserID: %v\n", claims[jwt.ClaimKeyUserID])

    // 刷新令牌
    newPair, err := j.RefreshToken(pair.RefreshToken)
    if err != nil {
        if errors.Is(err, jwt.ErrExpiredToken) {
            fmt.Println("refresh token expired, need re-login")
        } else {
            logger.Fatal("failed to refresh token", logger.Err(err))
        }
    }
    fmt.Printf("New Access: %s...\n", newPair.AccessToken[:30])

    // 作为 HTTP 中间件使用（httpx.WithJWT 或 j.AuthMiddleware 均可）
    _ = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}
```
