# project-structure

业务服务推荐目录结构，基于 infra-go 最佳实践总结。分层架构：**入口 → 配置 → 依赖装配 → 路由 → 处理器 → 业务逻辑 → 数据模型**。

## 推荐目录结构

```txt
my-service/
├── main.go                 # 入口：加载配置 → 创建 ServiceContext → 注册路由 → 启动服务
├── config/                 # 配置定义（struct + json 标签默认值/校验，配合 conf 加载）
│   └── config.go
├── svc/                    # 依赖装配层：ServiceContext
│   └── context.go
├── route/                  # 路由层：统一注册路由与全局/分组中间件
│   └── route.go
├── middleware/             # 中间件层：认证、审计、权限、上下文注入等 HTTP 中间件
│   ├── auth.go
│   ├── audit.go
│   └── context.go
├── handler/                # 处理器层：参数绑定 + 调用 Logic + 统一响应（薄层）
│   ├── account.go
│   └── order.go
├── logic/                  # 业务逻辑层：核心业务、事务、规则
│   ├── account.go
│   ├── order.go
│   └── store/              # 可选：存储接口与实现（如 KVStore 的 db/redis 后端）
├── model/                  # 数据模型层：GORM 实体、常量、DTO
│   ├── account.go
│   └── order.go
├── db/                     # 数据库层：迁移 + 种子数据
│   └── migrate.go
├── docs/                   # 设计文档
├── config.yaml             # 开发配置
├── config.docker.yaml      # 容器环境配置（可选）
├── Dockerfile
└── go.mod
```

## 分层职责与依赖方向

```txt
main.go ──► config ──► svc(ServiceContext) ──► route ──► middleware / handler
                    │                              │
                    └──► db / model ◄──────────────┴──► logic ──► model / logic/store
```

- 依赖方向**自上而下单向**：`handler` 依赖 `logic`，`logic` 依赖 `model`；禁止反向依赖。
- `middleware` 与 `handler` 平级，均依赖 `svc` 与 `logic`。
- `config` 是所有层的公共依赖；`svc` 把配置转成可用组件（DB/Redis/JWT/各 Logic/Handler）。

## 各目录说明

### config — 配置定义

只放配置结构体，用 `json` 标签声明默认值、范围、枚举，敏感项留空走环境变量：

```go
type Config struct {
    App      App                `json:"app"`
    Server   httpx.ServerConfig `json:"server"`
    DB       orm.Config         `json:"db"`
    JWT      jwt.Config         `json:"jwt"`
    Logger   logger.Config      `json:"logger"`
    Security SecurityConfig     `json:"security"`
    Redis    redisx.Config      `json:"redis,optional"`
}
```

### svc — 依赖装配（ServiceContext）

创建/注入/管理各组件（DB、Redis、JWT、Logic、Handler）生命周期，`main.go` 只做三件事——加载配置、创建 ServiceContext、启动服务：

```go
type ServiceContext struct {
    Config        config.Config
    DB            *gorm.DB
    JWT           *jwt.JWT
    RedisClient   redis.UniversalClient
    AuthLogic     *logic.AuthLogic
    OrderLogic    *logic.OrderLogic
    OrderHandler  *handler.OrderHandler
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
    // 按依赖顺序：orm.New → db.Migrate → jwt.New → redisx.New → 各 Logic/Handler
}

func (sc *ServiceContext) Close() { /* 关闭 DB / Redis / 后台 worker */ }
```

### route — 路由注册

统一注册所有路由与中间件链：

```go
func Register(server *httpx.Server, svcCtx *svc.ServiceContext) {
    server.Use(httpx.WithRequestID())
    server.Use(httpx.WithRecovery())
    server.Use(httpx.WithLogger())
    server.Use(httpx.WithTracing("/healthz"))

    v1 := server.Group("/api/v1")
    v1.AddRoute(httpx.Route{
        Method: http.MethodPost,
        Path:   "/orders",
        Handler: svcCtx.OrderHandler.Create,
    })
    // 受保护路由：追加鉴权中间件（见下）
}
```

### handler — 处理器层（薄层）

只做三件事：**参数绑定 → 调用 Logic → 统一响应**：

```go
func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    var req logic.CreateOrderRequest
    if err := httpx.MustBindJSON(w, r, &req); err != nil {
        return // 已自动写 400
    }
    order, err := h.svc.Create(ctx, &req)
    if err != nil {
        httpx.OkJSONCtx(ctx, w, httpx.NewCodeError(httpx.CodeDefaultError, err.Error()))
        return
    }
    httpx.OkJSONCtx(ctx, w, order)
}
```

### logic — 业务逻辑层

核心业务、事务、校验、规则；可依赖其他 Logic（经 ServiceContext 注入或接口解耦）。纯数据访问放 `logic/store/`。

### model — 数据模型

GORM 实体（表名、注释、索引）、常量、DTO，与 `db.Migrate` 对应。

### db — 迁移与种子

`Migrate(db)`：`AutoMigrate` 所有实体 + 种子数据（超级管理员、内置策略等）。

### middleware — 中间件

业务自定义中间件（认证、审计、权限、上下文注入）实现 `httpx.Middleware`（`func(http.HandlerFunc) http.HandlerFunc`）签名，供 `route` 挂载。

- 通用能力直接用 httpx 内置：`httpx.WithJWT`（JWT 认证）、`httpx.WithRateLimit`（限流）、`httpx.WithTracing`（链路）、`httpx.WithRecovery` 等。
- 需引入第三方标准 `func(http.Handler) http.Handler` 中间件时，用 `httpx.AsMiddleware` 包装后注册。
- handler 内读取当前用户 claims：`jwt.ClaimsFromContext(r.Context())`。

## 与 infra-go 模块的对应

| 目录 | 使用的 infra-go 模块 |
|------|------|
| `config` | `conf`（加载/默认值/校验） |
| `svc` | `orm` · `redisx` · `jwt` · `hash`（装配） |
| `route` | `httpx`（AddRoute/Group/Use） |
| `handler` | `httpx`（MustBind* / OkJSON / WriteHTTPError） |
| `middleware` | `httpx.WithJWT` · `httpx.WithRateLimit` · `httpx` 内置中间件 |
| `logic` | `orm` · `redisx` · `retry` · `taskq` · `syncx` · `cast` · `hash` |
| `main.go` | `conf` · `logger` · `httpx` · `service`（多服务编排） |

## 使用建议

- 小型服务（单一业务、<10 个接口）可合并 `handler` + `logic`，但建议保留 `svc` 与 `route` 分层。
- 有实时通信需求加 `ws/`（基于 `websocket`）；有异步任务加 `job/`（基于 `taskq`）。
- `config.yaml` 放开发默认值；敏感配置（JWT Secret、数据库密码）不写仓库，通过 `conf.UseEnv()` 的 `${VAR}` 覆盖。
