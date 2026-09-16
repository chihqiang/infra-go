# project-structure

Recommended directory structure for a business service, distilled from infra-go best practices. Layered architecture: **entry → config → dependency assembly → route → handler → business logic → data model**.

## Recommended directory structure

```txt
my-service/
├── main.go                 # Entry: load config → create ServiceContext → register routes → start server
├── config/                 # Config definitions (struct + json tag defaults/validation, loaded with conf)
│   └── config.go
├── svc/                    # Dependency assembly layer: ServiceContext
│   └── context.go
├── route/                  # Routing layer: registers all routes plus global/group middleware
│   └── route.go
├── middleware/             # Middleware layer: auth, audit, permissions, context injection, etc.
│   ├── auth.go
│   ├── audit.go
│   └── context.go
├── handler/                # Handler layer: bind params + call Logic + unified response (thin layer)
│   ├── account.go
│   └── order.go
├── logic/                  # Business logic layer: core business, transactions, rules
│   ├── account.go
│   ├── order.go
│   └── store/              # Optional: storage interfaces and implementations (e.g. KVStore db/redis backends)
├── model/                  # Data model layer: GORM entities, constants, DTOs
│   ├── account.go
│   └── order.go
├── db/                     # Database layer: migrations + seed data
│   └── migrate.go
├── docs/                   # Design documents
├── config.yaml             # Development config
├── config.docker.yaml      # Container environment config (optional)
├── Dockerfile
└── go.mod
```

## Layer responsibilities and dependency direction

```txt
main.go ──► config ──► svc(ServiceContext) ──► route ──► middleware / handler
                    │                              │
                    └──► db / model ◄──────────────┴──► logic ──► model / logic/store
```

- The dependency direction is **one-way, top-down**: `handler` depends on `logic`, `logic` depends on `model`; reverse dependencies are forbidden.
- `middleware` and `handler` are peers, both depending on `svc` and `logic`.
- `config` is a shared dependency of every layer; `svc` turns configuration into usable components (DB/Redis/JWT/each Logic/Handler).

## Directory details

### config — config definitions

Holds only config structs; declare defaults, ranges and enums with `json` tags, and leave sensitive items empty to be supplied via environment variables:

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

### svc — dependency assembly (ServiceContext)

Creates, injects and manages the lifecycle of each component (DB, Redis, JWT, Logic, Handler); `main.go` does only three things — load config, create the ServiceContext, start the server:

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
    // In dependency order: orm.New → db.Migrate → jwt.New → redisx.New → each Logic/Handler
}

func (sc *ServiceContext) Close() { /* close DB / Redis / background workers */ }
```

### route — route registration

Registers all routes and the middleware chain in one place:

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
    // Protected routes: append the auth middleware (see below)
}
```

### handler — handler layer (thin layer)

Does only three things: **bind params → call Logic → unified response**:

```go
func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    var req logic.CreateOrderRequest
    if err := httpx.MustBindJSON(w, r, &req); err != nil {
        return // 400 already written automatically
    }
    order, err := h.svc.Create(ctx, &req)
    if err != nil {
        httpx.OkJSONCtx(ctx, w, httpx.NewCodeError(httpx.CodeDefaultError, err.Error()))
        return
    }
    httpx.OkJSONCtx(ctx, w, order)
}
```

### logic — business logic layer

Core business, transactions, validation and rules; may depend on other Logic (injected via ServiceContext or decoupled through interfaces). Put pure data access in `logic/store/`.

### model — data model

GORM entities (table names, comments, indexes), constants and DTOs, matching `db.Migrate`.

### db — migrations and seeds

`Migrate(db)`: `AutoMigrate` all entities + seed data (super admin, built-in policies, etc.).

### middleware — middleware

Business-specific middleware (auth, audit, permissions, context injection) implements the `httpx.Middleware` signature (`func(http.HandlerFunc) http.HandlerFunc`) for `route` to mount.

- For common capabilities use the httpx built-ins directly: `httpx.WithJWT` (JWT auth), `httpx.WithRateLimit` (rate limiting), `httpx.WithTracing` (tracing), `httpx.WithRecovery`, etc.
- To bring in a third-party standard `func(http.Handler) http.Handler` middleware, wrap it with `httpx.AsMiddleware` before registering.
- Reading the current user's claims inside a handler: `jwt.ClaimsFromContext(r.Context())`.

## Mapping to infra-go modules

| Directory | infra-go modules used |
|------|------|
| `config` | `conf` (loading / defaults / validation) |
| `svc` | `orm` · `redisx` · `jwt` · `hash` (assembly) |
| `route` | `httpx` (AddRoute/Group/Use) |
| `handler` | `httpx` (MustBind* / OkJSON / WriteHTTPError) |
| `middleware` | `httpx.WithJWT` · `httpx.WithRateLimit` · httpx built-in middleware |
| `logic` | `orm` · `redisx` · `retry` · `taskq` · `syncx` · `cast` · `hash` |
| `main.go` | `conf` · `logger` · `httpx` · `service` (multi-service orchestration) |

## Recommendations

- Small services (single domain, <10 endpoints) may merge `handler` + `logic`, but keep the `svc` and `route` layers.
- Add `ws/` (based on `websocket`) when you need realtime communication; add `job/` (based on `taskq`) for async tasks.
- Keep development defaults in `config.yaml`; never commit sensitive config (JWT Secret, database password) — override it with the `${VAR}` support of `conf.UseEnv()`.
