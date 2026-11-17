---
name: infra-go-usage
description: 'Build services in business projects with the infra-go Go infrastructure library: conf (config loading), logger (structured logs), orm (MySQL/PostgreSQL/SQLite on gorm), redisx (Redis client, distributed lock), cache (in-memory and Redis), rest (HTTP server, method-aware routing, route groups, binding, unified JSON responses, middleware chain), rest/handler (recovery, request ID, tracing, access log, concurrency/timeout/body limits, gzip, breaker, rate limit, JWT auth, body signature), token (JWT), ratelimit, breaker, retry, taskq (async queue), storage (local/OSS/COS/KODO), ws (WebSocket), trace (OpenTelemetry), hash (bcrypt/MD5/SHA/HMAC/AES-GCM), cast, stringx, syncx, service (ServiceGroup). Use when: choosing, initializing or assembling infra-go modules in a Go service, combining conf+logger+orm+redisx+rest+token, or setting up unified responses, middleware and graceful shutdown.'
---

# infra-go usage guide

How to use each `github.com/chihqiang/infra-go` module correctly in a business project: module selection, initialization order, assembly and acceptance criteria. Applies to: starting a new Go business service, or wiring one infrastructure capability into an existing service.

## When to Use

- Starting a new Go business service that needs configuration, logging, database, Redis, HTTP and other infrastructure
- Deciding which infra-go module fits a requirement (see the decision table)
- Assembling several modules (such as conf + orm + http + token) correctly
- Handling errors, context and unified responses the way the project expects

## Module Decision Table

| Need | Module | Key entry point |
|------|------|----------|
| Load JSON/YAML config, defaults, environment variables | `conf` | `conf.MustLoad` |
| Structured / rotating logs | `logger` | package-level `logger.Info` or `logger.New` |
| Database CRUD (MySQL/Postgres/SQLite) | `orm` | `orm.MustNew` |
| Redis cache / distributed lock | `redisx` | `redisx.MustNew` |
| In-process memory cache (hot data) | `cache` | `cache.NewMemCache` |
| Distributed cache (shared across instances) | `cache` | `cache.NewRedisCache` |
| HTTP server / parameter binding / JSON responses | `rest` | `rest.NewServer` · `rest.Bind` · `rest.OkJSON` |
| Built-in HTTP middleware | `rest/handler` | `resthandler.NewRecoveryHandler(...).Handle` · `NewTimeoutHandler` · `NewLogHandler` · `NewRateLimitHandler` … (struct + `Config` + `Handle`) |
| Custom / third-party middleware | `rest` | `server.Use(chain.Middleware)` |
| API authentication | `token` | `token.NewParser` + `rest.WithRouteAuth(parser)` (or `resthandler.NewAuthHandler`) |
| API rate limiting (limiters) | `ratelimit` | `ratelimit.NewTokenBucket` / `NewRedisTokenBucket`; HTTP middleware `resthandler.NewRateLimitHandler` |
| Downstream protection (fail fast via breaker) | `breaker` | `breaker.NewBreaker` or `breaker.Do`; HTTP middleware `resthandler.NewBreakerHandler` |
| Failure retries | `retry` | `retry.Do` |
| Async task queue | `taskq` | `taskq.NewProducer` / `NewConsumer` |
| Object storage (local files / OSS/COS/KODO) | `storage` | `storage.New` |
| WebSocket realtime communication | `ws` | `ws.MustNew` |
| Distributed tracing (agent / span / propagation) | `trace` | `trace.StartAgent` |
| HTTP tracing middleware | `rest/handler` | `resthandler.NewTraceHandler(TraceConfig{...})` |
| Password / digest hashing | `hash` | `hash.BcryptHashDefault` |
| Sensitive-data encryption / request signing | `hash` | `hash.AESGCMEncrypt` / `hash.HMACSign` |
| Type-safe conversion | `cast` | `cast.To[T]` |
| String helpers | `stringx` | `stringx.RandId` |
| Concurrency primitives | `syncx` | `syncx.NewSingleFlight` |
| Start/stop several services concurrently | `service` | `service.NewServiceGroup` |

**Branching logic (decision points)**:

- Single-host rate limiting → in-memory limiter; shared across instances → Redis limiter; on HTTP mount `resthandler.NewRateLimitHandler(RateLimitConfig{Enabled: true, Limiter: limiter}).Handle`
- HTTP middleware: common capabilities live in `rest/handler` as structs with a `Config` and a `Handle(http.HandlerFunc) http.HandlerFunc` method, so they work both with `server.Use` and standalone; server-wide defaults are configured through one config struct per handler inside `rest.MiddlewaresConf`
- Short-lived temporary data (cache/session/counters) → `redisx`; reliable persistence and transactions → `orm`
- APIs that must respond synchronously → `rest` (plus `ws` when needed); async consumption is acceptable → `taskq`
- Mutual exclusion around a critical section → `redisx.Locker` (cross-instance) or `syncx` (single process)
- Preventing cache stampede / duplicate execution within one request → `syncx.NewSingleFlight`
- A unified interface for file upload/download → `storage` (the Driver decides local/OSS/COS/KODO)

## Workflow

### Step 1 — Requirement analysis: decide which modules are needed

1. List every infrastructure requirement of the service (config, logging, storage, HTTP, auth, rate limiting, retries, queues, realtime communication…)
2. Use the decision table to pick one module per requirement
3. Note the ordering dependencies between modules (e.g. token depends on the secret supplied by conf; ServiceGroup depends on each service's start/stop)
4. Lay out a skeleton following the recommended business directory structure (config / svc / route / handler / logic / model / middleware), see [project-structure](./references/project-structure.md)

### Step 2 — Add the dependencies

```bash
go get github.com/chihqiang/infra-go/core/conf
go get github.com/chihqiang/infra-go/core/logger
go get github.com/chihqiang/infra-go/rest
go get github.com/chihqiang/infra-go/core/orm
go get github.com/chihqiang/infra-go/core/redisx
go get github.com/chihqiang/infra-go/rest/token
go get github.com/chihqiang/infra-go/core/ratelimit
# ...import as needed
```

There are no mandatory dependencies between modules; import what you use — a module you never import doesn't enter the consumer's `go.mod` / `go.sum`.

> ⚠️ Exception at package granularity: the peer implementations in `storage` (three cloud SDKs),
> `orm` (three drivers) and `trace` (four exporter kinds) are bundled into a single package, so
> **you cannot pick just one** — importing `orm` and using only MySQL still compiles in the
> postgres / sqlite drivers (consumer `go.sum` 8 lines → 56 lines, binary 2.5M → 7.5M).
> If artifact size matters (container images, cold starts), bypass the wrapper and use
> `gorm.io/driver/*`, `go.opentelemetry.io/otel/exporters/*` or the cloud vendors' official SDKs directly.

### Step 3 — Define and load config with conf

```go
type Config struct {
    Host    string `json:",default=0.0.0.0"`
    Port    int    `json:",default=8080,range=[1:65535]"`
    LogMode string `json:",options=[file,console]"`
    Verbose bool   `json:",optional"`
}

var cfg Config
conf.MustLoad("config.yaml", &cfg, conf.UseEnv())
```

Tag directive quick reference: `default=...` default value; `range=[a:b]` numeric range; `options=[a,b]` enum; `optional` optional; `env=VAR` prefer the environment variable; inside the config, `${VAR}` references an environment variable (together with `conf.UseEnv()`).

### Step 4 — Initialize components in dependency order

Follow the convention that `New` returns an error and `MustNew` panics on error:

```go
// 1. Logging (global, initialized first)
logger.New(logger.Config{Level: "info", ServiceName: "my-service"})
defer logger.Sync()

// 2. Database
db := orm.MustNew(orm.Config{Driver: orm.DriverMySQL, Host: cfg.Host, ...})
defer orm.Close(db)

// 3. Redis
client := redisx.MustNew(redisx.Config{Addr: cfg.RedisAddr, KeyPrefix: "myapp"})
defer client.Close()

// 4. JWT (used by the auth middleware)
parser, err := token.NewParser(token.Config{Secret: cfg.JWTSecret})

// 5. Distributed tracing (optional). With rest, configure the agent on the
// trace middleware and NewServer starts/stops it:
// conf.Middlewares.Trace = resthandler.TraceConfig{
//     Enabled: true,
//     Agent: trace.Config{Name: "my-service", Endpoint: cfg.OtelEndpoint},
// }
// For non-rest programs use trace.StartAgent(...) + defer trace.StopAgent().
```

Convention: use `MustXxx` for global singletons / service-level components (panic if startup fails); use `Xxx` and handle the error explicitly for recoverable local objects; every component holding connections or background goroutines gets a `defer Close/Stop/Sync`.

### Step 5 — Assemble the HTTP server and middleware with http

1. Define request structs; implement `Validate` for checks that tags cannot express:

```go
type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

func (r *CreateUserRequest) Validate() error {
    if r.Name == "" {
        return rest.ErrBadRequest
    }
    return nil
}
```

2. Register routes; `binding.Bind` decodes by content type and `rest.ErrorJSON` maps failures to 400/415:

```go
server, err := rest.NewServer(cfg.Config) // embedded rest.Config from conf
if err != nil {
    return err
}

server.Group("/api").AddRoute(rest.Route{
    Method: http.MethodPost, Path: "/users",
    Handler: func(w http.ResponseWriter, r *http.Request) {
        var req CreateUserRequest
        if err := binding.Bind(r, &req); err != nil {
            _ = rest.ErrorJSON(w, err) // 400 / 415 already chosen from the error
            return
        }
        _ = rest.WriteJSON(r.Context(), w, http.StatusCreated, createUser(req))
    },
})
```

3. Pick middleware: config switches cover the common cases, `rest/handler` covers the rest:

```go
// config-driven (defaults in rest.Config / DefaultConfig)
conf := rest.DefaultConfig()
conf.Middlewares.Timeout = resthandler.TimeoutConfig{Enabled: true, Timeout: 5 * time.Second}
conf.Middlewares.MaxConns = resthandler.MaxConnsConfig{Enabled: true, Limit: 200}

// explicit mounting
server.Use(resthandler.NewTraceHandler(resthandler.TraceConfig{
	Enabled:     true,
	IgnorePaths: []string{"/healthz"},
}).Handle)
server.Use(resthandler.NewRateLimitHandler(resthandler.RateLimitConfig{
	Enabled: true,
	Limiter: ratelimit.NewTokenBucket(100, 200),
}).Handle) // 429
server.Use(resthandler.NewBreakerHandler(resthandler.BreakerConfig{
	Enabled: true,
	Breaker: br,
}).Handle) // 503 when open

// per-route auth
server.Group("/admin").Use(resthandler.NewAuthHandler(resthandler.AuthConfig{
	Enabled: true,
	Parser:  parser,
}).Handle)
```

> Third-party `func(http.Handler) http.Handler` middleware converts with `chain.Middleware(func(next http.HandlerFunc) http.HandlerFunc { return adapter(next).ServeHTTP })`; keep `rest/handler` handlers as the default choice since they already share the request-ID/logger conventions.

4. Start it (blocking). The library does not install OS signal handlers; wire
SIGINT/SIGTERM/SIGHUP to `Stop` yourself:

```go
go func() {
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
    <-sig
    _ = server.Stop() // or sg.Stop() when using service.ServiceGroup
}()
if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
    log.Fatal(err)
}
```

### Step 6 — Orchestrate the lifecycle of several services with service

```go
sg := service.NewServiceGroup()
server.Register(sg) // HTTP server + trace agent when configured
sg.Add(service.WithStart(func() { _ = consumer.Run() })) // taskq consumer: wrap it with WithStart
sg.Start() // blocking; returns once everything has exited; Stop is guaranteed to run only once
```

### Step 7 — Handle errors, context and unified responses the agreed way

- Error messages and comments in English (the project-wide style)
- At the HTTP layer use `rest.OkJSON` (wraps the value in the `code`/`msg`/`data` envelope, adding `request_id` when the request ID middleware runs) / `rest.WriteJSON(ctx, w, http.StatusCreated, body)` (raw body) for success, write bodyless statuses with `w.WriteHeader`, and use `rest.ErrorJSON(w, err)` for failures
- When trace/request correlation is needed, read the ID with `resthandler.RequestIDFromContext(r.Context())` and the client IP with `rest.ClientIP(r)`, then log with `logger.XxxCtx(ctx, ...)`
- Pass `context.Context` across modules consistently (orm / redisx / retry / taskq all support ctx timeouts and cancellation)
- Inspect semantic errors with `errors.Is`: `redisx.ErrNil`, `token.ErrInvalidToken`, `retry.ErrMaxRetries`, `rest.ErrResourceNotFound`, etc.

## Acceptance Criteria

- [ ] Only modules that are actually used have been imported
- [ ] Configuration is loaded through `conf`, with defaults and environment variable support; sensitive values come from environment variables
- [ ] All components are initialized in dependency order; connection-holding components have `Close / Stop / Sync`
- [ ] HTTP routes decode with `rest.Bind` and answer through `rest.OkJSON` / `rest.ErrorJSON`
- [ ] Auth / rate limiting / tracing / request ID / recovery are enabled via `rest.MiddlewaresConf` or mounted from `rest/handler`
- [ ] Custom middleware is written as `chain.Middleware` and mounted with `server.Use`
- [ ] Multiple services are started and stopped together with `service.NewServiceGroup`, with graceful shutdown
- [ ] Error messages and comments are English, matching the project style
- [ ] `retry` is used wherever network / transient failures occur; Redis rate limiting / locks are used in multi-instance setups
- [ ] `go build ./...` and `go vet ./...` pass

## References

The API documentation for each module is maintained in the `references/` directory (one file per module), covering installation, configuration, initialization, key methods and error conventions. HTTP server, routing, binding, handlers and JWT tokens are documented in [rest](./references/rest.md).

| Category | Module docs |
|------|------|
| Configuration and logging | [conf](./references/conf.md) · [logger](./references/logger.md) |
| Data layer | [orm](./references/orm.md) · [redisx](./references/redisx.md) · [cache](./references/cache.md) |
| HTTP and APIs | [rest](./references/rest.md) (server, routing, binding, handler, token) · [ratelimit](./references/ratelimit.md) · [breaker](./references/breaker.md) · [retry](./references/retry.md) · [ws](./references/ws.md) |
| Async and storage | [taskq](./references/taskq.md) · [storage](./references/storage.md) |
| Observability and security | [trace](./references/trace.md) · [hash](./references/hash.md) |
| General utilities | [cast](./references/cast.md) · [stringx](./references/stringx.md) · [syncx](./references/syncx.md) |
| Service orchestration | [service](./references/service.md) · [mapping](./references/mapping.md) |
| Project structure | [project-structure](./references/project-structure.md) |

> `x` and `respw` no longer exist: client-IP resolution is internal to `rest` (public helper `rest.ClientIP`), and request IDs are available through `rest/handler.RequestIDFromContext`; response-writer wrappers live in `rest/internal/response`. See [rest](./references/rest.md).

All modules follow the same conventions: `New` returns an error and `MustNew` panics on error; connection-holding components have `Close / Stop / Sync`; comments and errors in English.
