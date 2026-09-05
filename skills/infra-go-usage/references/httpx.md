# httpx

HTTP 服务基础设施，位于 `httpx` 目录。主包提供服务器、统一响应、请求绑定便捷函数与中间件适配；核心能力按职责拆分到多个子包，均可被其它 `net/http` 兼容框架复用。

## 模块结构

```text
httpx/
├── request.go              — 请求绑定便捷 API（Bind*/MustBind*）+ 单值读取（QueryValue/PathValue/HeaderValue）
├── response.go             — 统一响应：Response[T]/CodeError/Ok*/Write*/SSEWriter/Redirect*
├── server.go               — 服务器：Server/Route/Group/路由注册/中间件链/优雅关闭
├── internal_middleware.go  — 中间件适配层：With*（转发 httpx/middleware）+ AsMiddleware
├── internal_route.go       — 内置路由（PprofRoutes）
├── ctx.go                  — request_id context 工具（委托 httpx/middleware）
├── binding/                — 请求绑定实现（绑定器/映射引擎/校验器，见下）
├── middleware/             — 通用中间件实现（面向对象，一个中间件一个文件）
├── match/                  — 路径匹配器（中间件 skip/ignore 规则，见 [match](./match.md)）
└── respw/                  — ResponseWriter 增强包装（见 [respw](./respw.md)）
```

- `httpx/binding`：绑定器接口/实例、MIME 常量、反射映射引擎、校验器（`SetValidateFn`）。
- `httpx/middleware`：各中间件核心逻辑，`NewXxx(...)` + `Middleware()` 面向对象形态，返回标准 `func(http.Handler) http.Handler`，不依赖 httpx，可被 gin / echo 等直接复用（详见「中间件」节）。
- `httpx` 主包 = 服务器 + 统一响应 + 绑定便捷函数 + `With*` 适配层，是业务项目主要使用入口。

## 特性

- **统一响应**：`Response[T]` 结构 + `Ok*` / `WriteHTTPError*` 智能包装
- **参数绑定**：JSON / XML / Form / Query / Header / URI 六种来源 + `binding` 标签校验
- **通用中间件**：CORS、Recovery、RequestID、链路追踪、访问日志、熔断、超时、请求体限制、gzip 解压、并发数限制、限流、JWT 认证、加解密、内容安全
- **可复用**：中间件核心逻辑在 `httpx/middleware` 子包，标准 `net/http` 形态，可被任何框架复用
- **Server 路由**：Go 1.22 `{param}` 路径参数、路由组、中间件链、优雅关闭、pprof

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
    "github.com/chihqiang/infra-go/httpx/middleware" // 可选：直接用子包中间件
)

func main() {
    server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})

    // 中间件（With* 便捷注册，等价于 middleware.NewXxx().Middleware() 经 AsMiddleware 接入）
    server.Use(httpx.WithRecovery())
    server.Use(httpx.WithRequestID())
    server.Use(httpx.WithLogger("/healthz"))
    server.Use(httpx.WithCors("*"))

    server.AddRoute(httpx.Route{
        Method: "POST",
        Path:   "/users",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req CreateUserRequest
            if err := httpx.MustBindJSON(w, r, &req); err != nil {
                return // 已自动写 400
            }
            httpx.OkJSON(w, map[string]any{"id": "user-1"}) // 自动包成 Response[T]
        },
    })

    server.Start() // 阻塞；SIGINT/SIGTERM/SIGHUP 优雅关闭
}
```

## 请求绑定

将请求数据按来源绑定到结构体。字段通过 tag 指定来源名，并可用 `binding` 标签声明校验规则。

> 绑定器的**实现**在 `httpx/binding` 子包（绑定器实例 `binding.JSON/XML/Form/Query/Header/Uri`、`binding.Default` 选择器、反射映射引擎与 `SetValidateFn` 校验入口）。httpx 主包 `Bind*` / `MustBind*` 便捷函数内部调用该子包，业务侧通常直接用主包函数即可。

### 支持的标签

| 标签 | 适用来源 | 说明 |
|------|---------|------|
| `json` | JSON body | JSON 字段名 |
| `xml` | XML body | XML 字段名 |
| `form` | Form / Query | 表单 / Query 参数名 |
| `uri` | URI | 路径参数名 |
| `header` | Header | HTTP 请求头名（大小写不敏感） |
| `binding` | 全部 | 校验规则，见[参数验证](#参数验证) |
| `default` | Form / Query / Header / URI | 字段默认值，如 `form:"sort,default=desc"` |
| `time_format` / `time_utc` / `time_location` | Form / Query | 时间解析控制 |
| `-` | 全部 | 忽略该字段，不做绑定 |

### 绑定函数一览

| 函数 | 数据来源 | 说明 |
|------|---------|------|
| `BindJSON(r, &obj)` | JSON body | |
| `BindXML(r, &obj)` | XML body | |
| `BindForm(r, &obj)` | Query + POST form | |
| `BindQuery(r, &obj)` | URL Query | |
| `BindHeader(r, &obj)` | HTTP Header | |
| `BindURI(params, &obj)` | 路径参数 | `params` 为 `map[string]string` |
| `BindURIWithValues(params, &obj)` | 路径参数 | `params` 为 `map[string][]string` |
| `Bind(r, &obj)` | 自动 | 按 Method / Content-Type 选择 |
| `MustBind*` | — | 绑定 + 自动错误响应 |

### 示例

```go
// JSON
type LoginRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required,min=6"`
}
var req LoginRequest
if err := httpx.BindJSON(r, &req); err != nil {
    httpx.WriteHTTPError(w, http.StatusBadRequest, err.Error())
    return
}

// Query（form 标签，支持 default）
type ListRequest struct {
    Page     int    `form:"page" binding:"gte=1"`
    PageSize int    `form:"page_size" binding:"gte=1,lte=100"`
    Sort     string `form:"sort,default=desc"`
}
if err := httpx.BindQuery(r, &req); err != nil { /* handle */ }

// Header
type AuthRequest struct {
    Token   string `header:"X-Auth-Token" binding:"required"`
    Version string `header:"X-Version,default=v1"`
}
if err := httpx.BindHeader(r, &req); err != nil { /* handle */ }

// URI（路径参数 /users/{id}）
type GetUserRequest struct {
    ID int `uri:"id" binding:"required"`
}
params := map[string]string{"id": r.PathValue("id")}
if err := httpx.BindURI(params, &req); err != nil { /* handle */ }
```

### 自动选择规则

`Bind(r, &obj)` 按 Method 与 Content-Type 选择：GET → Form（Query）；POST + `application/json` → JSON；`application/xml`/`text/xml` → XML；`application/x-www-form-urlencoded` / `multipart/form-data` → Form；其它/无法解析 → Form。

### MustBind — 绑定 + 自动错误响应

绑定或校验失败时自动写入 HTTP 错误响应（携带 `request_id`）并返回 error 供控制流判断：

```go
if err := httpx.MustBindJSON(w, r, &req); err != nil {
    return // 已自动写 400
}
// 同系列：MustBind（自动选择）/ MustBindQuery / MustBindForm
```

### 单值读取 — QueryValue / PathValue / HeaderValue

按 key 读取单个值并转换类型（底层复用 `cast.ToE`），无需定义结构体：

```go
page  := httpx.QueryValue(r, "page", 1)     // int，缺失/非法 → 1
tag   := httpx.QueryValue[string](r, "tag") // string，缺失 → ""
id    := httpx.PathValue(r, "id", int64(0)) // 路径参数 {id}
token := httpx.HeaderValue(r, "X-Token", "")
```

## 参数验证

绑定器映射后自动校验 `binding` 标签规则（基于 [go-playground/validator/v10](https://github.com/go-playground/validator)）：

```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=20"`
    Password string `json:"password" binding:"required,min=8"`
    Email    string `json:"email" binding:"required,email"`
    Role     string `json:"role" binding:"required,oneof=admin user guest"`
}
```

常用规则：`required` 必填；`min=N`/`max=N`；`gte=N`/`lte=N`；`email`/`url`；`oneof=a b c` 枚举；`len=N`。

默认校验器为 `httpx/binding` 子包的 `DefaultValidator`（标签 `binding`）。需要自定义校验器时，实现 `binding.StructValidator` 并通过 `binding.SetValidateFn` 注入校验入口（见附录）；传 `nil` 恢复默认。httpx 主包便捷绑定函数与 binding 绑定器共享同一校验入口。

## 支持的数据类型

| 类型 | 示例 |
|------|------|
| `string` | `Name string \`form:"name"\`` |
| `int/int8/int16/int32/int64`、`uint/.../uint64` | `Age int \`form:"age"\`` |
| `bool` | `Active bool \`form:"active"\`` |
| `float32/float64` | `Score float64 \`form:"score"\`` |
| `time.Time` | 支持 `time_format`/`time_utc`/`time_location` 与 unix 时间戳 |
| `time.Duration` | `Timeout time.Duration \`form:"timeout"\``（如 `1m30s`） |
| `[]string` 等切片 | 逗号分隔或重复 key 自动收集 |
| 内嵌结构体 / `map[string]string` 目标 | 递归 / 直接填充 |

## 统一响应

### Response[T] 结构

```go
type Response[T any] struct {
    Code      int    `json:"code" xml:"code"`
    Msg       string `json:"msg" xml:"msg"`
    Data      T      `json:"data,omitempty" xml:"data,omitempty"`
    RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
}
```

### 智能包装（推荐）

`Ok*` 系列自动把 `v` 包进 `Response[T]`；若 `v` 是 `*CodeError` / `CodeError` / `error`，自动取对应业务码与消息：

```go
httpx.OkJSON(w, data)          // {code:0,msg:"ok",data:...}
httpx.OkJSONCtx(ctx, w, data)  // 响应携带 request_id
httpx.OkXML(w, data)
httpx.OkXMLCtx(ctx, w, data)
httpx.OkHTML(w, "<h1>hi</h1>")
httpx.OkHTMLCtx(ctx, w, "<h1>hi</h1>")
```

### 底层输出

```go
httpx.WriteJSON(w, status, v)        // 任意状态码 + 任意结构
httpx.WriteJSONCtx(ctx, w, status, v)
httpx.WriteXML(w, status, v)
httpx.WriteXMLCtx(ctx, w, status, v)
```

### 错误响应

```go
httpx.WriteHTTPError(w, status, msg)                       // code = status
httpx.WriteHTTPErrorCtx(ctx, w, status, msg)               // 带 request_id
httpx.WriteHTTPErrorWithCode(w, status, code, msg)         // 业务码与 HTTP 码分离
httpx.WriteHTTPErrorWithCodeCtx(ctx, w, status, code, msg)
```

业务码常量：`CodeOK = 0`、`MsgOK = "ok"`、`CodeDefaultError = -1`。

### CodeError

```go
httpx.NewCodeError(code, msg)                 // 业务错误
httpx.NewCodeErrorWithCause(code, msg, err)   // 带根因，可 errors.Is/As
```

### SSE 流式响应

```go
sse := httpx.NewSSEWriter(w, r)
sse.SendEvent("message", map[string]any{"a": 1}) // data: {...}
sse.SendData(v)        // 序列化后写 data
sse.Comment("keepalive")
sse.Retry(3000)        // 断线重连间隔
sse.Flush()
```

> `WithTimeout` 对 SSE/WebSocket 长连接豁免。

### 重定向

```go
httpx.Redirect(w, r, url, http.StatusFound)   // RedirectCtx / RedirectTemporary / RedirectTemporaryCtx
```

## 请求 ID（Request ID）

`WithRequestID` 从 `X-Request-Id` 头读取（缺省自动生成 uuid），注入 context 并回写响应头。下游读取：

```go
id := httpx.RequestIDFromContext(r.Context())
```

> request_id 的 context key 与存取实现统一在 `httpx/middleware`（`middleware.ContextWithRequestID`/`RequestIDFromContext`），httpx `ctx.go` 委托转发；配合 `OkJSONCtx`/`WriteHTTPErrorCtx` 会自动出现在响应 `request_id` 字段，配合 `logger.XxxCtx` 自动出现在日志。

## 路由与服务器

### 服务器创建

```go
server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})
```

### Route 与注册

```go
server.AddRoute(httpx.Route{Method: "GET", Path: "/users", Handler: handler})
server.AddRoutes([]httpx.Route{{...}, ...}, httpx.WithPrefix("/api/v1"))
server.Routes() // 返回全部路由副本
```

### 路径参数

Go 1.22 路由模式 `{param}`：

```go
server.AddRoute(httpx.Route{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    httpx.OkJSON(w, id)
}})
```

### 中间件

```go
type Middleware func(http.HandlerFunc) http.HandlerFunc

server.Use(mw1, mw2)              // 全局中间件（对所有路由生效）
server.AddRoutes(rs, httpx.WithMiddleware(authMW)) // 单路由组中间件
server.AddRoutes(httpx.ApplyMiddleware(authMW, routes...)) // 直接包装路由
```

执行顺序：全局中间件（`Use`）→ 组中间件 → 路由 handler。

### 路由组（Group）

```go
api := server.Group("/api", authMW)
v1 := api.Group("/v1", logMW) // 前缀 /api/v1，中间件叠加
v1.AddRoute(httpx.Route{...})
```

### 自定义 404

```go
server.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
    httpx.OkJSON(w, "not found")
})
```

### panic 恢复

`WithRecovery` 捕获 handler panic，记录堆栈并返回 500。

### 启动与关闭

```go
server.Start()    // 阻塞，SIGINT/SIGTERM/SIGHUP 优雅关闭
server.Shutdown() // 手动关闭
```

## 中间件

httpx 中间件分两层：

1. **`httpx/middleware` 子包（实现层）**：一个中间件一个文件、一个类型；`NewXxx(...)` 构造（构造时完成参数预计算），`(m *Xxx) Middleware()` 返回标准 `func(http.Handler) http.Handler`。不依赖 httpx，可被 gin/echo/标准库复用。错误响应经 `middleware.SetErrorHandler` 注入（httpx 主包 init 注入统一 JSON）。
2. **`httpx` 主包（适配层）**：`WithXxx(...)` 便捷函数把子包标准中间件适配为 `httpx.Middleware` 供 `server.Use` 注册，方法签名稳定。

### 内置中间件清单（httpx.With*）

| 中间件 | 用途 |
|--------|------|
| `WithCors(origins...)` | CORS；`"*"` 全放行；同源不设头；未授权 403；OPTIONS 204 |
| `WithRecovery()` | panic 恢复 → 500 + 堆栈日志 |
| `WithRequestID()` | request_id 注入 context / 回写响应头 |
| `WithTracing(ignorePaths...)` | 链路追踪（服务端 span，默认全局 TracerProvider） |
| `WithLogger(skipPaths...)` | 访问日志（method/path/status/bytes/latency） |
| `WithBreaker()` | 全局限流熔断（全局单一熔断器） |
| `WithRouteBreaker()` | 按路由 `METHOD:path` 隔离熔断 |
| `WithTimeout(d)` | 请求超时（WS/SSE 豁免；客户端断开 499） |
| `WithMaxBytes(n)` | 请求体大小限制（413） |
| `WithGunzip()` | gzip 请求体自动解压 |
| `WithMaxConns(n)` | 并发连接数限制（503） |
| `WithRateLimit(limiter, skipPaths...)` | 限流（429；limiter 来自 ratelimit 包，见 [ratelimit](./ratelimit.md)） |
| `WithJWT(j, getToken)` | JWT 认证（转发 `jwt.AuthMiddleware`，见 [jwt](./jwt.md)） |
| `WithCryption(key, skipPaths...)` | 请求/响应 AES-GCM 加解密 |
| `WithContentSecurity(key, tolerance)` | 内容安全校验（防篡改 + 防重放） |

用法：

```go
server.Use(httpx.WithRecovery(), httpx.WithRequestID(), httpx.WithLogger("/healthz"))
server.Use(httpx.WithCors("*"))
server.Use(httpx.WithTracing("/health*", "/metrics/*"))   // 放最前，日志带 trace_id
server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))
server.Use(httpx.WithJWT(j, func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
server.Use(httpx.WithTimeout(5 * time.Second))
```

### 直接使用 httpx/middleware 子包（其它框架 / 标准库）

```go
import "github.com/chihqiang/infra-go/httpx/middleware"

// 标准 net/http
handler := middleware.NewRecovery().Middleware()(
    middleware.NewRequestID().Middleware()(mux),
)
http.ListenAndServe(":8080", handler)

// gin：用 WrapH 接入
router.Use(gin.WrapH(middleware.NewCORS("*").Middleware()(router)))
```

错误响应机制：`middleware.WriteError(ctx, w, status, msg)`（导出）。httpx 主包被 import 时输出统一 JSON（携带 request_id）；否则默认 `http.Error` 纯文本。gin/echo 等可用 `middleware.SetErrorHandler` 注入自己的错误渲染。

### 自定义 / 第三方标准中间件接入 httpx

`httpx.AsMiddleware(mw)` 把任意标准 `func(http.Handler) http.Handler` 中间件适配为 `httpx.Middleware`：

```go
server.Use(httpx.AsMiddleware(myStdMiddleware))                            // 自定义/第三方
server.Use(httpx.AsMiddleware(middleware.NewCORS("*").Middleware()))       // 子包 OO 形态
```

### skipPaths / ignorePaths 匹配

`WithLogger` / `WithRateLimit` / `WithCryption` 的 `skipPaths`、`WithTracing` 的 `ignorePaths` 使用 `httpx/match` 子包的 `PathMatcher`，支持精确匹配（`/health`）、前缀通配（`/health*`，跨目录）、glob（`/api/*/x`，不跨目录），见 [match](./match.md)。

## 内置路由：PprofRoutes

```go
server.AddRoutes(httpx.PprofRoutes(""))                 // 默认前缀 /debug/pprof
server.AddRoutes(httpx.PprofRoutes(""), httpx.WithMiddleware(authMW)) // 生产建议加认证
```

## Server 配置

```go
server := httpx.NewServer(httpx.ServerConfig{
    Host: "0.0.0.0", Port: 8080,
    ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
    IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20,
    ShutdownTimeout: 10 * time.Second,
})
```

或 `RunOption` 编程式覆盖：

```go
httpx.NewServer(httpx.ServerConfig{...},
    httpx.WithReadTimeout(10*time.Second),
    httpx.WithWriteTimeout(10*time.Second),
    httpx.WithIdleTimeout(120*time.Second),
    httpx.WithMaxHeaderBytes(1<<20),
)
```

## 附录

### 自定义校验器

```go
import "github.com/chihqiang/infra-go/httpx/binding"

type myValidator struct{}

func (v *myValidator) ValidateStruct(obj any) error { return nil }
func (v *myValidator) Engine() any                  { return nil }

binding.SetValidateFn((&myValidator{}).ValidateStruct) // 接入自定义校验
binding.SetValidateFn(nil)                             // 恢复默认（go-playground/validator）
```

### 绑定器直接使用（httpx/binding）

```go
import "github.com/chihqiang/infra-go/httpx/binding"

var obj MyReq
_ = binding.JSON.BindBody([]byte(`{...}`), &obj)   // 从原始字节绑定
b := binding.Default(r.Method, r.Header.Get("Content-Type")) // 绑定器选择
```

### 常见中间件封装示例

```go
// 封装「绑定 + 校验 + 权限」的处理器（供路由挂载）
func authzMW(roles ...string) httpx.Middleware {
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            claims := jwt.ClaimsFromContext(r.Context())
            role, _ := claims[jwt.ClaimKeyRole].(string)
            if !slices.Contains(roles, role) {
                httpx.WriteHTTPErrorCtx(r.Context(), w, http.StatusForbidden, "forbidden")
                return
            }
            next(w, r)
        }
    }
}
```
