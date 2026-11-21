# project-structure

Recommended directory structure for a business service, distilled from ten-go best practices. Layered architecture: **entry → config → dependency assembly → route → handler → business logic → data model**.

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
// rest.Config is embedded, not carried in a separate HTTP/Server field: conf and
// mapping flatten anonymous embedded structs, so the server keys live at the top
// level of config.yaml and main.go passes the embedded value straight to NewServer.
type Config struct {
    rest.Config
    App      App            `json:"app"`
    DB       orm.Config     `json:"db"`
    Token    token.Config   `json:"token"`
    Security SecurityConfig `json:"security"`
    Redis    redisx.Config  `json:"redis,optional"`
}
```

```yaml
# config.yaml — server keys are top level because rest.Config is embedded;
# omitted sections still get their struct-tag defaults
host: 0.0.0.0
port: 8080
readTimeout: 10s
app:
  name: my-service
db:
  driver: mysql
```

```go
server, err := rest.NewServer(cfg.Config) // the embedded rest.Config
```

- Do not add a separate `Server rest.Config` / `HTTP rest.Config` field: it forces `cfg.Server.Port` at every call site and nests all server settings under one config key.
- Do not add a second `Logger` field either — the embedded `rest.Config` already provides one; an outer field would shadow it and leave two keys competing for the same `logger` section.
- Embedding promotes `rest.Config.Validate`, so `conf` validates host/port/TLS and the settings of enabled middleware automatically. A business-level `Validate` must call the embedded one first: `if err := c.Config.Validate(); err != nil { return err }`.

### svc — dependency assembly (ServiceContext)

Creates, injects and manages the lifecycle of each component (DB, Redis, JWT, Logic, Handler); `main.go` does only three things — load config, create the ServiceContext, start the server:

```go
type ServiceContext struct {
    Config        config.Config
    DB            *gorm.DB
    Token         *token.Parser
    RedisClient   redis.UniversalClient
    AuthLogic     *logic.AuthLogic
    OrderLogic    *logic.OrderLogic
    OrderHandler  *handler.OrderHandler
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
    // In dependency order: orm.New → db.Migrate → token.NewParser → redisx.New → each Logic/Handler
}

func (sc *ServiceContext) Close() { /* close DB / Redis / background workers */ }
```

### route — route registration

Registers all routes and the middleware chain in one place:

```go
func Register(server *rest.Server, svcCtx *svc.ServiceContext) {
    // request id / recovery / access log / trace are switched on by
    // rest.DefaultConfig(); mount extras here
    server.Use(resthandler.NewTraceHandler(resthandler.TraceConfig{
        Enabled:     true,
        IgnorePaths: []string{"/healthz"},
        ServiceName: "my-service",
    }).Handle)

    v1 := server.Group("/api/v1")
    v1.AddRoute(rest.Route{
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
    if err := rest.Bind(r, &req); err != nil {
        _ = rest.ErrorJSONCtx(ctx, w, err) // 400/415 chosen from the error
        return
    }
    order, err := h.svc.Create(ctx, &req)
    if err != nil {
        _ = rest.ErrorJSONCtx(ctx, w, err)
        return
    }
    _ = rest.OkJSONCtx(ctx, w, order)
}
```

### logic — business logic layer

Core business, transactions, validation and rules; may depend on other Logic (injected via ServiceContext or decoupled through interfaces). Put pure data access in `logic/store/`.

### model — data model

GORM entities (table names, comments, indexes), constants and DTOs, matching `db.Migrate`.

### db — migrations and seeds

`Migrate(db)`: `AutoMigrate` all entities + seed data (super admin, built-in policies, etc.).

### middleware — middleware

Business-specific middleware (auth, audit, permissions, context injection) implements `chain.Middleware` (`func(http.HandlerFunc) http.HandlerFunc`) for `route` to mount.

- For common capabilities use the rest built-ins directly: `rest.WithRouteAuth` (JWT auth), `resthandler.NewRateLimitHandler` (rate limiting), `resthandler.NewTraceHandler` (tracing), `resthandler.NewRecoveryHandler`, etc.
- To bring in a third-party standard `func(http.Handler) http.Handler` middleware, wrap it into `chain.Middleware` before calling `server.Use`.
- Reading the current user's custom claims inside a handler: `token.Claim(r.Context(), "uid")`.

## Mapping to ten-go modules

| Directory | ten-go modules used |
|------|------|
| `config` | `conf` (loading / defaults / validation) |
| `svc` | `orm` · `redisx` · `token` · `hash` (assembly) |
| `route` | `rest` (AddRoute/Group/Use) |
| `handler` | `rest` (Bind / OkJSONCtx / ErrorJSONCtx) |
| `middleware` | `rest.WithRouteAuth` · `resthandler.NewRateLimitHandler` · rest built-in handlers |
| `logic` | `orm` · `redisx` · `retry` · `taskq` · `syncx` · `cast` · `hash` |
| `main.go` | `conf` · `logger` · `rest` · `service` (multi-service orchestration) |

## Recommendations

- Small services (single domain, <10 endpoints) may merge `handler` + `logic`, but keep the `svc` and `route` layers.
- Add `ws/` (based on `ws`) when you need realtime communication; add `job/` (based on `taskq`) for async tasks.
- Keep development defaults in `config.yaml`; never commit sensitive config (JWT Secret, database password) — override it with the `${VAR}` support of `conf.UseEnv()`.
