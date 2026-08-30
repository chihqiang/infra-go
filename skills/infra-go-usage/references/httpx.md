# httpx

HTTP 工具包，提供请求参数绑定（参考 gin 的设计）和统一响应。

## 特性

- **路由服务器**：基于 `http.ServeMux`，原生支持方法匹配、路径参数、通配路径
- **中间件链**：全局中间件 + 组中间件，执行顺序清晰可控
- **路由分组**：`WithPrefix` + `WithMiddleware` 或链式 `Group`，支持嵌套
- **优雅关闭**：SIGINT/SIGTERM/SIGHUP 信号触发，可配置超时，关闭失败记录日志
- **配置驱动**：`ServerConfig` 使用 `json` 标签声明默认值，兼容 `conf` 包从文件加载
- **参数绑定**：支持 JSON、XML、Query、Form、Header、URI 六种绑定方式
- **自动选择**：根据 Method 和 Content-Type 自动选择绑定器
- **参数验证**：基于 `go-playground/validator/v10`，使用 `binding` 标签
- **默认值**：通过 `default=xxx` 标签选项设置字段默认值
- **泛型响应**：`Response[T]` 统一响应结构，data 字段类型安全
- **请求 ID**：`WithRequestID` 中间件自动生成/透传 `request_id`，`Ctx` 响应函数自动携带到响应中
- **智能包装**：`OkJSON` / `OkXML` 自动识别 error / CodeError / 普通数据
- **多格式响应**：支持 JSON、XML、HTML 三种响应格式
- **重定向**：`Redirect` 系列函数，支持 301/302 及自定义状态码
- **自定义 404 响应**：`SetNotFoundHandler` 自定义未匹配路由的响应内容
- **Context 变体**：每个响应函数都有对应的 `Ctx` 版本
- **类型丰富**：支持 int/uint/bool/float/string/time.Time/time.Duration/slice/指针等
- **MustBind**：绑定+验证+自动写入错误响应，一步到位
- **cast 集成**：布尔和 Duration 类型转换使用 `cast` 包，统一类型转换逻辑

## 安装

```bash
go get github.com/chihqiang/infra-go/httpx
```

## 快速开始

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/httpx"
)

type CreateUserRequest struct {
    Name  string `json:"name" binding:"required"`
    Email string `json:"email" binding:"required,email"`
    Age   int    `json:"age" binding:"gte=0,lte=150"`
}

type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    server := httpx.NewServer(httpx.ServerConfig{
        Host: "0.0.0.0",
        Port: 8080,
    })

    server.AddRoute(httpx.Route{
        Method: "POST",
        Path:   "/users",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req CreateUserRequest
            // 绑定 + 验证，出错自动写入 400 响应
            if err := httpx.MustBindJSON(w, r, &req); err != nil {
                return
            }

            user := User{ID: 1, Name: req.Name, Email: req.Email}
            // 智能包装：自动设置 code=0, msg=ok, data=user
            httpx.OkJSON(w, user)
        },
    })

    server.Start() // 阻塞，支持优雅关闭（SIGINT/SIGTERM/SIGHUP）
}
```

## 请求参数绑定

### 支持的标签

| 标签 | 适用绑定 | 说明 |
| ------ | --------- | ------ |
| `json` | JSON, XML | JSON 字段名 |
| `form` | Form, Query | 表单/Query 参数名 |
| `uri` | URI | 路径参数名 |
| `header` | Header | HTTP Header 名 |
| `binding` | 全部 | 验证规则（基于 validator） |
| `default` | Form, Query, Header | 默认值，如 `form:"sort,default=desc"` |
| `time_format` | Form, Query | 时间格式，如 `time_format:"2006-01-02"` |

### BindJSON - JSON Body 绑定

```go
type LoginRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required,min=6"`
}

var req LoginRequest
if err := httpx.BindJSON(r, &req); err != nil {
    httpx.WriteHTTPError(w, http.StatusBadRequest, err.Error())
    return
}
```

### BindQuery - URL Query 绑定

```go
type ListRequest struct {
    Page     int    `form:"page" binding:"required,gte=1"`
    PageSize int    `form:"page_size" binding:"required,gte=1,lte=100"`
    Keyword  string `form:"keyword"`
    Sort     string `form:"sort,default=desc"`
}

var req ListRequest
if err := httpx.BindQuery(r, &req); err != nil {
    // handle error
}
```

### BindForm - 表单绑定

```go
type UploadRequest struct {
    Title       string   `form:"title" binding:"required"`
    Description string   `form:"description"`
    Tags        []string `form:"tags"`
}

var req UploadRequest
if err := httpx.BindForm(r, &req); err != nil {
    // handle error
}
```

### BindHeader - Header 绑定

```go
type AuthRequest struct {
    Token   string `header:"X-Auth-Token" binding:"required"`
    TraceID string `header:"X-Trace-Id"`
    Version string `header:"X-Version,default=v1"`
}

var req AuthRequest
if err := httpx.BindHeader(r, &req); err != nil {
    // handle error
}
```

### BindURI - 路径参数绑定

```go
type GetUserRequest struct {
    ID int `uri:"id" binding:"required"`
}

// params 通常来自路由解析，如 /users/:id => {"id": "123"}
params := map[string]string{"id": "123"}
var req GetUserRequest
if err := httpx.BindURI(params, &req); err != nil {
    // handle error
}
```

### Bind - 自动选择绑定器

根据 Method 和 Content-Type 自动选择：

```go
var req MyRequest
if err := httpx.Bind(r, &req); err != nil {
    // handle error
}
```

选择规则：

- GET 请求 → Form 绑定（Query 参数）
- POST + `application/json` → JSON 绑定
- POST + `application/x-www-form-urlencoded` → Form 绑定
- POST + `multipart/form-data` → Form 绑定

### MustBind 系列 - 绑定 + 自动错误响应

```go
var req CreateUserRequest
// 出错自动写入 400 响应，返回 error 供控制流判断
if err := httpx.MustBindJSON(w, r, &req); err != nil {
    return
}

// 同系列函数
httpx.MustBind(w, r, &req)         // 自动选择
httpx.MustBindQuery(w, r, &req)    // Query
httpx.MustBindForm(w, r, &req)     // Form
httpx.MustBindURI(w, params, &req) // URI
```

## 参数验证

基于 [go-playground/validator/v10](https://github.com/go-playground/validator)，使用 `binding` 标签：

```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=20"`
    Password string `json:"password" binding:"required,min=8"`
    Email    string `json:"email" binding:"required,email"`
    Age      int    `json:"age" binding:"gte=18,lte=120"`
    Role     string `json:"role" binding:"required,oneof=admin user guest"`
}
```

常用验证规则：

| 规则 | 说明 |
| ------ | ------ |
| `required` | 必填 |
| `min=N` | 最小长度/最小值 |
| `max=N` | 最大长度/最大值 |
| `gte=N` | 大于等于 |
| `lte=N` | 小于等于 |
| `email` | 邮箱格式 |
| `oneof=a b c` | 枚举值 |
| `url` | URL 格式 |
| `len=N` | 长度等于 |

## 支持的数据类型

绑定支持以下 Go 类型：

| 类型 | 示例 |
| ------ | ------ |
| `string` | `` Name string `form:"name"` `` |
| `int/int8/int16/int32/int64` | `` Age int `form:"age"` `` |
| `uint/uint8/.../uint64` | `` Count uint `form:"count"` `` |
| `bool` | `` Active bool `form:"active"` `` |
| `float32/float64` | `` Score float64 `form:"score"` `` |
| `time.Duration` | `` Timeout time.Duration `form:"timeout"` `` |
| `time.Time` | `` CreatedAt time.Time `form:"created_at" time_format:"2006-01-02"` `` |
| `[]string` / `[]int` 等 | `` Tags []string `form:"tags"` `` |
| 指针类型 | `` Name *string `json:"name"` `` |
| 嵌套结构体 | 自动递归处理 |
| `map[string]string` | 通过 JSON 解析 |

### 时间格式

```go
type TimeRequest struct {
    // RFC3339 格式（默认）
    CreatedAt time.Time `form:"created_at"`
    // 自定义格式
    Birthday time.Time `form:"birthday" time_format:"2006-01-02"`
    // Unix 时间戳
    Timestamp time.Time `form:"timestamp" time_format:"unix"`
    // UTC 时区
    UTCTime time.Time `form:"utc_time" time_format:"2006-01-02" time_utc:"true"`
}
```

### 切片绑定

Query 参数 `tags=a,b,c` 会自动分割为 `["a", "b", "c"]`：

```go
type SearchRequest struct {
    Tags []string `form:"tags"` // tags=golang,redis,mysql => ["golang","redis","mysql"]
}
```

## 统一响应

### Response 泛型结构

```go
type Response[T any] struct {
    Code      int    `json:"code" xml:"code"`                     // 业务码，0 表示成功
    Msg       string `json:"msg" xml:"msg"`                       // 提示信息
    Data      T      `json:"data,omitempty" xml:"data,omitempty"` // 响应数据
    RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"` // 请求 ID（可选）
}
```

### 智能包装（推荐）

`OkJSON` 和 `OkXML` 会根据传入值的类型自动包装为统一响应：

```go
// 1. 传入普通数据 → code=0, msg=ok, data=数据
httpx.OkJSON(w, user)
// 输出: {"code":0,"msg":"ok","data":{"name":"Alice"}}

// 2. 传入 *CodeError → 使用其 Code 和 Msg
httpx.OkJSON(w, httpx.NewCodeError(httpx.CodeNotFound, "user not found"))
// 输出: {"code":404,"msg":"user not found"}

// 3. 传入普通 error → code=-1, msg=错误信息
httpx.OkJSON(w, errors.New("database error"))
// 输出: {"code":-1,"msg":"database error"}
```

### JSON 响应

```go
// 智能包装 + HTTP 200
httpx.OkJSON(w, data)

// 带 context 的版本
httpx.OkJSONCtx(ctx, w, data)

// 低级：直接序列化写入，不做包装
httpx.WriteJSON(w, http.StatusCreated, data)

// HTTP 错误响应（设置 HTTP 状态码）
httpx.WriteHTTPError(w, http.StatusNotFound, "not found")

// 分离 HTTP 状态码与业务码（适用于需要细粒度业务错误码的场景）
// HTTP 400，业务码 10001
httpx.WriteHTTPErrorWithCode(w, http.StatusBadRequest, 10001, "username already exists")
```

#### WriteHTTPErrorWithCode / WriteHTTPErrorWithCodeCtx

在 RESTful API 中，HTTP 状态码反映传输层状态（如 400 Bad Request），而业务码反映业务语义（如 10001 表示"用户名已存在"）。`WriteHTTPErrorWithCode` 允许二者独立设置：

```go
// HTTP 400，业务码 10001
httpx.WriteHTTPErrorWithCode(w, http.StatusBadRequest, 10001, "username already exists")
// 输出: {"code":10001,"msg":"username already exists"}

// 带 context 的版本（自动携带 request_id）
httpx.WriteHTTPErrorWithCodeCtx(ctx, w, http.StatusBadRequest, 10001, "username already exists")
```

### XML 响应

```go
// 智能包装 + HTTP 200，带 XML 声明
httpx.OkXML(w, data)
// 输出: <xml version="1.0" encoding="UTF-8"><code>0</code><msg>ok</msg><data>...</data></xml>

// 带 context 的版本
httpx.OkXMLCtx(ctx, w, data)

// 低级：直接序列化写入
httpx.WriteXML(w, http.StatusOK, data)
```

### HTML 响应

```go
// HTML 200
httpx.OkHTML(w, "<h1>Hello</h1>")

// 带 context 的版本
httpx.OkHTMLCtx(ctx, w, "<h1>Hello</h1>")

// 指定状态码
httpx.WriteHTML(w, http.StatusOK, "<h1>Hello</h1>")
```

### 重定向

`Redirect` 系列函数基于 `http.Redirect`，自动设置 `Location` 响应头：

```go
// 指定状态码重定向
httpx.Redirect(w, r, "/login", http.StatusFound)

// 带 context 的版本
httpx.RedirectCtx(ctx, w, r, "/login", http.StatusFound)

// 便捷封装：临时重定向（302 Found）
httpx.RedirectTemporary(w, r, "/temporary")
httpx.RedirectTemporaryCtx(ctx, w, r, "/temporary")

// 便捷封装：永久重定向（301 Moved Permanently）
httpx.RedirectPermanent(w, r, "/permanent")
httpx.RedirectPermanentCtx(ctx, w, r, "/permanent")
```

| 函数 | 状态码 | 说明 |
| ---- | ------ | ---- |
| `Redirect(w, r, url, status)` | 自定义 | 指定状态码重定向 |
| `RedirectTemporary(w, r, url)` | 302 | 临时重定向 |
| `RedirectPermanent(w, r, url)` | 301 | 永久重定向 |

> 注意：`Redirect` 系列函数需要 `*http.Request` 参数，因此没有 `OkJSON` 那样的 `w` 单参版本。

### 业务码

| 码 | 常量 | 说明 |
| ---- | ------ | ------ |
| 0 | `CodeOK` | 成功 |
| -1 | `CodeDefaultError` | 默认错误码 |
| 400 | `CodeBadRequest` | 参数错误 |
| 401 | `CodeUnauthorized` | 未认证 |
| 403 | `CodeForbidden` | 无权限 |
| 404 | `CodeNotFound` | 资源不存在 |
| 413 | `CodeRequestEntityTooLarge` | 请求体过大 |
| 500 | `CodeInternalError` | 服务器内部错误 |
| 503 | `CodeServiceUnavailable` | 服务不可用 |
| 504 | `CodeTimeout` | 请求超时 |

### CodeError

```go
// 创建错误
err := httpx.NewCodeError(httpx.CodeNotFound, "user not found")

// 带原始错误
err := httpx.NewCodeErrorWithCause(httpx.CodeInternalError, "database error", dbErr)

// 直接传入 OkJSON，自动识别
httpx.OkJSON(w, err)
// 输出: {"code":404,"msg":"user not found"}

// 支持 errors.Is / errors.As
if errors.Is(err, dbErr) {
    // ...
}
```

## JSON 解析

```go
// 直接解析 JSON body
var req MyRequest
if err := httpx.ParseJSON(r, &req); err != nil {
    // handle error
}

// 限制 body 大小（防止超大请求）
if err := httpx.ParseJSONWithLimit(r, &req, 1<<20); err != nil {
    // handle error
}
```

## 请求 ID（Request ID）

用于链路追踪与问题排查。`WithRequestID` 中间件从 `X-Request-Id` 请求头读取，不存在则自动生成，注入 context 并回写响应头。

```go
// 启用中间件（建议放在最前）
server.Use(httpx.WithRequestID())
```

`Ctx` 响应函数会自动将 `request_id` 写入响应：

```go
func handler(w http.ResponseWriter, r *http.Request) {
    // context 由中间件注入了 request_id
    httpx.OkJSONCtx(r.Context(), w, user)              // {"code":0,"msg":"ok","data":...,"request_id":"a1b2c3d4"}
    httpx.OkXMLCtx(r.Context(), w, user)               // XML 同样携带
    httpx.WriteHTTPErrorCtx(r.Context(), w, 404, "not found") // {"code":404,"msg":"not found","request_id":"..."}
}
```

### 手动注入

非 HTTP 场景可手动注入/提取：

```go
ctx := httpx.ContextWithRequestID(context.Background(), "req-123")
id := httpx.RequestIDFromContext(ctx) // "req-123"
```

> 注意：
> - 无 `request_id` 时 `RequestID` 字段自动省略（`omitempty`），不改变原有输出结构
> - 只有智能包装函数（`OkJSONCtx` / `OkXMLCtx` / `WriteHTTPErrorCtx`）会注入；低级函数 `WriteJSONCtx` / `WriteXMLCtx` 不做包装，传什么写什么
> - 中间件回写 `X-Request-Id` 响应头，客户端可拿到本次请求的 ID 用于上报排查

## 路由与服务器

基于 `http.ServeMux` 实现，原生支持方法匹配、路径参数和通配路径。
不内置任何中间件，仅提供路由注册、前缀分组、中间件链和优雅关闭。

### 基本示例

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/httpx"
)

func main() {
    server := httpx.NewServer(httpx.ServerConfig{
        Host: "0.0.0.0",
        Port: 8080,
    })

    server.AddRoute(httpx.Route{
        Method: "GET",
        Path:   "/health",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            httpx.OkJSON(w, "ok")
        },
    })

    server.Start() // 阻塞，支持优雅关闭（SIGINT/SIGTERM/SIGHUP）
}
```

### 闭包注册路由

`RunOption` 本身就是 `func(*Server)`，可直接传入闭包，在构造时执行任意 Server 方法：

```go
server := httpx.NewServer(httpx.ServerConfig{
    Host: "0.0.0.0",
    Port: 8080,
}, func(s *httpx.Server) {
    s.Use(loggingMiddleware)

    api := s.Group("/api/v1", authMiddleware)
    api.AddRoute(httpx.Route{Method: "GET", Path: "/users", Handler: listUsers})
    api.AddRoute(httpx.Route{Method: "POST", Path: "/users", Handler: createUser})

    s.AddRoute(httpx.Route{Method: "GET", Path: "/health", Handler: healthCheck})
})

server.Start()
```

### Route

```go
type Route struct {
    Method  string           // HTTP 方法：GET/POST/PUT/DELETE/...
    Path    string           // 路径，支持 {id} 参数和 {path...} 通配
    Handler http.HandlerFunc // 处理函数
}
```

### 路由注册

```go
// 单个路由
server.AddRoute(httpx.Route{
    Method: "POST", Path: "/users", Handler: createUser,
})

// 批量注册（共享前缀和中间件）
server.AddRoutes([]httpx.Route{
    {Method: "GET",    Path: "/users",     Handler: listUsers},
    {Method: "GET",    Path: "/users/{id}", Handler: getUser},
    {Method: "DELETE", Path: "/users/{id}", Handler: deleteUser},
}, httpx.WithPrefix("/api/v1"))
```

### 路由前缀

```go
server.AddRoutes(routes, httpx.WithPrefix("/api/v1"))
// /users → /api/v1/users
// /users/{id} → /api/v1/users/{id}
```

### 中间件

中间件类型：`func(http.HandlerFunc) http.HandlerFunc`

执行顺序：**全局中间件 → 组中间件 → handler**

```go
// 日志中间件
func logging(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next(w, r)
        log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
    }
}

// 全局中间件（对所有路由生效）
server.Use(logging)

// 组中间件（仅对这组路由生效）
server.AddRoutes(adminRoutes,
    httpx.WithPrefix("/admin"),
    httpx.WithMiddleware(authMiddleware),
    httpx.WithMiddleware(rateLimitMiddleware),
)

// 多个中间件一起添加
server.AddRoutes(routes, httpx.WithMiddlewares(mw1, mw2, mw3))
```

#### 中间件短路

中间件不调用 `next` 即中断链路：

```go
func auth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") == "" {
            httpx.WriteHTTPError(w, http.StatusUnauthorized, "unauthorized")
            return // 不调用 next，handler 不会执行
        }
        next(w, r)
    }
}
```

#### 独立包装函数

对特定路由单独包装中间件：

```go
server.AddRoutes(httpx.ApplyMiddleware(authMiddleware,
    httpx.Route{Method: "GET", Path: "/profile", Handler: getProfile},
    httpx.Route{Method: "PUT", Path: "/profile", Handler: updateProfile},
))

// 多个中间件
server.AddRoutes(httpx.ApplyMiddlewares([]httpx.Middleware{authMW, logMW},
    httpx.Route{Method: "GET", Path: "/settings", Handler: getSettings},
))
```

#### 内置中间件

httpx 提供两个常用中间件，直接开箱即用：

##### WithRecovery

捕获 handler 中的 panic，记录堆栈并返回 500，防止进程崩溃：

```go
server.Use(httpx.WithRecovery())
```

panic 时会通过 `logger` 包记录错误日志（含 method、path、remote、stack），并返回统一格式的 500 响应。

##### WithLogger

记录每个请求的方法、路径、状态码、响应字节数和耗时，基于 `logger` 包的结构化日志：

```go
server.Use(httpx.WithLogger())
```

输出示例：

```json
{"level":"INFO","msg":"http request","method":"GET","path":"/api/users","status":200,"bytes":42,"latency":"1.2ms"}
```

配合 `trace` 包使用时，日志会自动带上 `trace_id`、`span_id`（logger 的 Ctx 提取器自动提取）：

```go
import _ "github.com/chihqiang/infra-go/trace" // 自动注册 trace 提取器

server.Use(httpx.WithRecovery(), httpx.WithLogger())
```

##### WithCors

设置 CORS 响应头，处理跨域请求和 OPTIONS 预检：

```go
// 允许所有来源
server.Use(httpx.WithCors("*"))

// 允许指定来源
server.Use(httpx.WithCors("https://example.com", "https://app.example.com"))
```

##### WithBreaker

熔断保护，防止下游故障级联拖垮服务（基于 `breaker` 模块的 Google SRE 算法）：

```go
server.Use(httpx.WithBreaker())
```

熔断打开时请求立即返回 503，不再等待慢速下游；请求成功（<500）计入成功，失败（>=500）计入失败，驱动熔断状态自动流转。

##### WithRouteBreaker

按路由隔离的熔断保护，每个路由（`METHOD:path`）拥有独立的熔断器，统计互不影响，避免单个路由的失败拉低其他路由的通过率：

```go
server.Use(httpx.WithRouteBreaker())
```

熔断器通过 `breaker.GetBreaker` 按名称缓存，同名路由共享同一实例。适用于不同路由下游依赖差异较大的场景。

##### WithTimeout

请求超时控制，每个请求最多执行指定时长，超时返回 503；WebSocket / SSE 长连接不受限制：

```go
server.Use(httpx.WithTimeout(5 * time.Second))
```

`duration <= 0` 时中间件不生效。客户端主动断开返回非标准状态码 499（nginx 约定）。

##### WithMaxBytes

限制请求体大小，超过限制返回 413；对分块传输请求在读取时限制：

```go
server.Use(httpx.WithMaxBytes(1 << 20)) // 限制 1MB
```

`n <= 0` 表示不限制。

##### WithGunzip

自动解压 gzip 请求体（`Content-Encoding: gzip`），解压失败返回 400：

```go
server.Use(httpx.WithGunzip())
```

##### WithMaxConns

限制同时处理的请求数，防止连接耗尽；超限返回 503：

```go
server.Use(httpx.WithMaxConns(1000))
```

`n <= 0` 表示不限制。

##### WithCryption

AES-GCM 请求/响应加密中间件：请求体为 base64 编码的 AES-GCM 密文（`nonce || ciphertext`），解密后交给 handler；handler 的响应会被加密后返回：

```go
// 密钥必须为 16/24/32 字节（AES-128/192/256）
server.Use(httpx.WithCryption([]byte("0123456789abcdef")))
```

AES-GCM 是认证加密（AEAD），同时保证机密性与完整性（防篡改），nonce 每次随机生成。

> **大响应保护**：响应超过 1MB 时自动回退为明文输出（不加密），并记录告警日志，避免大响应导致内存暴涨。

##### WithContentSecurity

内容安全校验中间件（防篡改 + 防重放）。客户端需在 `X-Content-Security` 头携带签名 `time=<unix秒>; signature=<base64 HMAC-SHA256>`，签名内容为 `timestamp\nmethod\npath\nquery\nbodySha256Hex`：

```go
server.Use(httpx.WithContentSecurity([]byte("shared-secret"), 5*time.Minute))
```

- 签名无效 → 401
- 时间戳超出容差（防重放）→ 403
- 校验通过 → 放行

##### 链路追踪

OpenTelemetry 链路追踪中间件由 `trace` 包提供（`trace.HTTPMiddleware()`，返回标准 `func(http.Handler) http.Handler`），从请求头提取上游 span 上下文（W3C traceparent），为每个请求创建服务端 span，并注入 context 供下游 `logger` / `orm` / `redisx` 等模块自动关联 `trace_id`：

```go
// 包装为 httpx.Middleware 使用
server.Use(func(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        trace.HTTPMiddleware()(http.HandlerFunc(next)).ServeHTTP(w, r)
    }
})
```

### 路由组（Group）

链式创建路由组，支持嵌套：

```go
// 方式一：RouteOption
server.AddRoutes([]httpx.Route{
    {Method: "GET", Path: "/users", Handler: listUsers},
}, httpx.WithPrefix("/api/v1"), httpx.WithMiddleware(authMW))

// 方式二：Group（更简洁）
api := server.Group("/api/v1", authMW)
api.AddRoute(httpx.Route{Method: "GET", Path: "/users", Handler: listUsers})
api.AddRoute(httpx.Route{Method: "POST", Path: "/users", Handler: createUser})

// 嵌套子组（继承父组前缀和中间件）
v1 := api.Group("/v1") // 前缀 /api/v1/v1，中间件 authMW
v2 := api.Group("/v2", rateLimitMW) // 前缀 /api/v1/v2，中间件 authMW → rateLimitMW
```

### 路径参数

基于 `http.ServeMux` 原生路径参数，直接使用 `r.PathValue`：

```go
server.AddRoute(httpx.Route{
    Method: "GET",
    Path:   "/users/{id}",
    Handler: func(w http.ResponseWriter, r *http.Request) {
        id := r.PathValue("id") // "42"
        httpx.OkJSON(w, id)
    },
})

// 通配路径
server.AddRoute(httpx.Route{
    Method: "GET",
    Path:   "/files/{path...}",
    Handler: func(w http.ResponseWriter, r *http.Request) {
        p := r.PathValue("path") // "dir/sub/file.txt"
        httpx.OkJSON(w, p)
    },
})
```

#### 路径参数绑定

```go
server.AddRoute(httpx.Route{
    Method: "GET",
    Path:   "/users/{id}/posts/{postID}",
    Handler: func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            ID     int `uri:"id" binding:"required"`
            PostID int `uri:"postID" binding:"required"`
        }
        // 收集路径参数，绑定+验证，出错写入 400 响应
        params := map[string]string{
            "id":     r.PathValue("id"),
            "postID": r.PathValue("postID"),
        }
        if err := httpx.MustBindURI(w, params, &req); err != nil {
            return
        }
        httpx.OkJSON(w, req)
    },
})
```

### 自定义 404 响应

`SetNotFoundHandler` 用于自定义 404 响应，替代默认的 `404 page not found`：

```go
// 自定义 404 响应：所有未被路由匹配的请求都会走这里
server.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
    httpx.WriteHTTPError(w, http.StatusNotFound, "resource not found: "+r.URL.Path)
})
```

执行顺序：

```text
mux → 404 拦截 → 全局中间件
```

- 404 拦截紧贴 mux，只对真正未匹配的请求生效，不影响 405 Method Not Allowed 和已注册路由

### panic 恢复

路由或中间件抛出 panic 时，使用内置 `WithRecovery` 中间件即可捕获并返回 500，避免连接被 net/http 直接关闭：

```go
server.Use(httpx.WithRecovery())
```

### Server 配置

`ServerConfig` 使用 `json` 标签声明默认值和约束，兼容 `conf` 包从配置文件加载：

```go
type ServerConfig struct {
    Host            string        `json:",default=0.0.0.0"`
    Port            int           `json:",default=8080,range=[1:65535]"`
    CertFile        string        `json:",optional"`
    KeyFile         string        `json:",optional"`
    ReadTimeout     time.Duration `json:",default=10s"`
    WriteTimeout    time.Duration `json:",default=10s"`
    IdleTimeout     time.Duration `json:",default=120s"`
    MaxHeaderBytes  int           `json:",default=1048576"`
    ShutdownTimeout time.Duration `json:",default=10s"`
}
```

代码中使用（零值字段自动填充默认值）：

```go
server := httpx.NewServer(httpx.ServerConfig{
    Host:     "0.0.0.0",
    Port:     8080,
    CertFile: "cert.pem",  // TLS 证书（可选，设置后启用 HTTPS）
    KeyFile:  "key.pem",   // TLS 私钥（可选）
}, httpx.WithReadTimeout(30*time.Second))
```

从配置文件加载（配合 `conf` 包）：

```go
var cfg httpx.ServerConfig
conf.MustLoad("config.yaml", &cfg)
server := httpx.NewServer(cfg)
```

```yaml
# config.yaml
host: 0.0.0.0
port: 8080
readTimeout: 30s
writeTimeout: 30s
```

#### RunOption（编程式覆盖）

RunOption 优先级高于配置文件，用于代码中动态覆盖：

| 选项 | 说明 |
| ------ | ------ |
| `WithReadTimeout(d)` | 读超时 |
| `WithWriteTimeout(d)` | 写超时 |
| `WithIdleTimeout(d)` | 空闲连接超时 |
| `WithMaxHeaderBytes(n)` | 最大请求头字节数 |
| `WithTLSConfig(cfg)` | TLS 配置 |
| `WithShutdownTimeout(d)` | 优雅关闭超时 |

### 启动与关闭

```go
// 启动（阻塞，收到 SIGINT/SIGTERM/SIGHUP 自动优雅关闭）
// 收到信号时记录日志，Shutdown 失败时记录错误日志
if err := server.Start(); err != nil {
    log.Fatal(err)
}

// 手动关闭
if err := server.Shutdown(); err != nil {
    log.Printf("shutdown error: %v", err)
}
```

### 路由查看

```go
// 打印已注册路由
server.PrintRoutes()
// 输出：
// DELETE  /admin/users/{id}   --> main.deleteUser
// GET     /api/v1/users       --> main.listUsers
// GET     /api/v1/users/{id}  --> main.getUser
// GET     /health             --> main.health
// POST    /api/v1/users       --> main.createUser
//
// 5 routes registered

// 获取已注册路由
routes := server.Routes()
for _, r := range routes {
    fmt.Printf("%s %s\n", r.Method, r.Path)
}
```

## 目录结构

```text
httpx/
├── bind.go              — 公开 API：Bind*, MustBind*, Validate, ParseJSON*
├── binding.go           — 绑定器实现：接口定义、MIME 常量、内置绑定器、反射映射、验证器
├── response.go          — 公开 API：Response[T], CodeError, OkJSON/OkXML/OkHTML, Write*, Redirect*
├── server.go            — 公开 API：Route, Middleware, Server, Group, WithPrefix, Start/Shutdown, SetNotFoundHandler
├── route.go             — 独立路由函数：PprofRoutes
├── middleware.go        — 内置中间件：WithRecovery, WithLogger, WithCors, WithRequestID, WithBreaker, WithTimeout, WithMaxBytes, WithGunzip, WithMaxConns, WithCryption, WithContentSecurity
├── responsewriter.go    — 自定义 ResponseWriter（内部使用）：statusRecorder 捕获状态码/字节数、timeoutWriter 超时缓冲丢弃、cryptionResponseWriter 响应加密缓冲，均透传 Flush/Hijack/Push
├── ctx.go               — context 工具：ContextWithRequestID / RequestIDFromContext
├── httpx_test.go
├── middleware_test.go   — 全部中间件测试（含加密/内容安全）
├── responsewriter_test.go
└── server_test.go
```

> 绑定逻辑全部合并在 `binding.go` 单文件中，不再使用子包。布尔和 Duration 类型转换使用 `cast` 包，整数和浮点类型保留 `strconv` 以支持位宽溢出检查。AES-GCM 加密与 HMAC 签名/校验由 `hash` 包提供（见 [hash](./hash.md)）。

## 自定义验证器

```go
import "github.com/chihqiang/infra-go/httpx"

// 实现 StructValidator 接口
type myValidator struct{}

func (v *myValidator) ValidateStruct(obj any) error {
    // 自定义验证逻辑
    return nil
}

func (v *myValidator) Engine() any {
    return nil
}

// 替换全局验证器
httpx.Validator = &myValidator{}
```

## HTTP 中间件示例

本包不内置 HTTP 中间件，但可以很方便地自行封装：

```go
// BindJSONMiddleware 自动绑定 JSON 请求体
func BindJSONMiddleware(handler func(http.ResponseWriter, *http.Request, *MyRequest)) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req MyRequest
        if err := httpx.MustBindJSON(w, r, &req); err != nil {
            return // MustBindJSON 已自动写入错误响应
        }
        handler(w, r, &req)
    }
}
```
