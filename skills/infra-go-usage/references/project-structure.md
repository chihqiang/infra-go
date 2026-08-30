# project-structure

业务服务推荐目录结构。基于 infra-go 的最佳实践总结，采用分层架构：**入口 → 配置 → 依赖装配 → 路由 → 处理器 → 业务逻辑 → 数据模型**。

适用于：新建基于 infra-go 的 Go 业务服务时，作为目录骨架与各层职责的推荐模板。

## 推荐目录结构

```txt
my-service/
├── main.go                 # 入口：加载配置 → 创建 AppContext → 注册路由 → 启动服务
├── config/                 # 配置定义（struct + json 标签默认值/校验，配合 conf 加载）
│   └── config.go
├── svc/                    # 依赖装配层：AppContext
│   └── appcontext.go
├── route/                  # 路由层：统一注册路由与全局/分组中间件
│   └── route.go
├── middleware/             # 中间件层：认证、审计、权限、上下文等 HTTP 中间件
│   ├── auth.go
│   ├── audit.go
│   └── context.go
├── handler/                # 处理器层：参数绑定 + 调用 Logic + 统一响应（薄层）
│   ├── account.go
│   └── order.go
├── logic/                  # 业务逻辑层：核心业务、事务、规则（可含 store/ 子包）
│   ├── account.go
│   ├── order.go
│   └── store/              # 可选：存储接口与实现（如 KVStore 的 db/redis 后端）
├── model/                  # 数据模型层：GORM 实体、常量、DTO
│   ├── account.go
│   └── order.go
├── db/                     # 数据库层：迁移 + 种子数据
│   └── migrate.go
├── docs/                   # 设计文档（OAuth 集成、权限设计等）
├── config.yaml             # 开发配置
├── config.docker.yaml      # 容器环境配置（可选）
├── Dockerfile
└── go.mod
```

## 分层职责与依赖方向

```txt
main.go ──► config ──► svc(AppContext) ──► route ──► middleware / handler
                    │                              │
                    └──► db / model ◄──────────────┴──► logic ──► model / logic/store
```

- 依赖方向**自上而下单向**：`handler` 依赖 `logic`，`logic` 依赖 `model`；禁止反向依赖。
- `middleware` 与 `handler` 平级，均依赖 `svc` 与 `logic`。
- `config` 是所有层的公共依赖，`svc` 负责把配置转成可用的组件（DB/Redis/JWT/各 Logic/Handler）。

## 各目录说明

### config — 配置定义

只放配置结构体，用 `json` 标签声明默认值、范围、枚举，敏感项留空走环境变量。

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

### svc — 依赖装配（AppContext）

将应用所需组件（数据库 / Redis / 加密 / JWT / 各业务 Logic 与 Handler）的创建、注入与生命周期管理统一收敛到一处，`main.go` 只做三件事——加载配置、创建 AppContext、启动服务。

```go
type AppContext struct {
    Config config.Config
    DB          *gorm.DB
    JWT         *jwt.JWT
    RedisClient redis.UniversalClient
    AuthLogic   *logic.AuthLogic
    OrderLogic  *logic.OrderLogic
    OrderHandler *handler.OrderHandler
    // ...
}

func NewAppContext(c config.Config) (*AppContext, error) {
    // 按依赖顺序：orm.New → db.Migrate → jwt.New → redisx.New → 各 NewXxxLogic → 各 NewXxxHandler
}

func (s *AppContext) Close() { /* 关闭 DB / Redis / 后台 worker */ }
```

### route — 路由注册

统一在此注册所有路由与中间件链，包含路由规划注释。组合 `httpx` 的 `AddRoute` / `Group` / `Use` 能力。

```go
func Register(server *httpx.Server, ctx *svc.AppContext) {
    server.Use(httpx.WithRequestID())
    server.Use(httpx.WithRecovery())
    server.Use(httpx.WithLogger())

    v1 := server.Group("/api/v1")
    v1.AddRoute(httpx.Route{
        Method: http.MethodPost,
        Path:   "/orders",
        Handler: ctx.OrderHandler.Create,
    })
    // 受保护路由：追加鉴权 / 权限中间件
}
```

### handler — 处理器层（薄层）

只做三件事：**参数绑定 → 调用 Logic → 统一响应**。不写业务规则。

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

核心业务、事务、校验、规则都在这层；可依赖其他 Logic（通过 AppContext 注入，或接口解耦如缓存失效回调）。纯数据访问放 `logic/store/` 子包（存储接口 + 多后端实现）。

### model — 数据模型

GORM 实体（含表名、注释、索引）、业务常量、DTO。与 `db.Migrate` 对应。

```go
type Account struct {
    ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:主键ID"`
    Name      string    `json:"name" gorm:"size:64;uniqueIndex;not null;comment:名称"`
    CreatedAt time.Time `json:"created_at" gorm:"comment:创建时间"`
    // ...
}
```

### db — 迁移与种子

`Migrate(db)`：`AutoMigrate` 所有实体 + 种子数据（超级管理员、内置策略等）。

### middleware — 中间件

认证（`AuthMiddleware`）、审计、权限、上下文注入等；实现 `httpx.Middleware` 签名，供 `route` 挂载。

## 与 infra-go 模块的对应

| 目录 | 使用的 infra-go 模块 |
|------|------|
| `config` | `conf`（加载/默认值/校验） |
| `svc` | `orm` · `redisx` · `jwt` · `hash`（装配） |
| `route` | `httpx`（AddRoute/Group/Use） |
| `handler` | `httpx`（MustBind* / OkJSON / WriteHTTPError） |
| `middleware` | `jwt.AuthMiddleware` · `ratelimit` · `httpx` 中间件 |
| `logic` | `orm` · `redisx` · `retry` · `taskq` · `syncx` · `cast` · `hash` |
| `main.go` | `conf` · `logger` · `httpx` · `service`（多服务编排） |

## 使用建议

- 小型服务（单一业务、<10 个接口）可以合并：`handler` + `logic` 合并为 `api` 或直接放 `handler`；但仍建议保留 `svc` 与 `route` 分层。
- 有实时通信需求时增加 `ws/`（基于 `websocket`）；有异步任务增加 `job/`（基于 `taskq`）。
- `config.yaml` 放开发默认值；敏感配置（JWT Secret、数据库密码）不写进仓库，通过 `conf.UseEnv()` 的 `${VAR}` 覆盖。
