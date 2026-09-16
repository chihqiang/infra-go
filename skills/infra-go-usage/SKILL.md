---
name: infra-go-usage
description: 'Build services in business projects with the infra-go Go infrastructure library. Covers conf (config loading), logger (structured logging), orm (MySQL/PostgreSQL/SQLite), redisx (Redis and distributed locks), cache (unified cache: in-memory/Redis), httpx (HTTP server, parameter binding, unified Response[T] responses, built-in middleware: breaker/timeout/rate limiting/encryption/load shedding/JWT auth, core in the httpx/middleware subpackage), jwt (JWT wrapping and authentication), ratelimit (in-memory/Redis limiters), breaker (circuit breaker), retry (retries), taskq (async task queue), storage (unified object storage: local files/OSS/COS/KODO, write/read/exists/delete/URL), websocket (realtime communication), trace (distributed tracing: agent/span/propagation), hash (password/digest/encryption/HMAC signing), cast (type conversion), stringx (strings), syncx (concurrency primitives), service (ServiceGroup service orchestration). Use when: writing a Go business service and you need to choose, initialize or assemble infra-go modules, combine conf+logger+orm+redisx+httpx+jwt, or set up unified responses, middleware, graceful shutdown and other infrastructure.'
---

# infra-go usage guide

How to use each `github.com/chihqiang/infra-go` module correctly in a business project: module selection, initialization order, assembly and acceptance criteria. Applies to: starting a new Go business service, or wiring one infrastructure capability into an existing service.

## When to Use

- Starting a new Go business service that needs configuration, logging, database, Redis, HTTP and other infrastructure
- Deciding which infra-go module fits a requirement (see the decision table)
- Assembling several modules (such as conf + orm + httpx + jwt) correctly
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
| HTTP server / parameter binding / unified responses | `httpx` | `httpx.NewServer` |
| General HTTP middleware (breaker/timeout/encryption/tracing…) | `httpx` | `httpx.WithRecovery` · `httpx.WithRateLimit` · `httpx.WithTracing` … |
| Custom / third-party standard middleware integration | `httpx` | `httpx.AsMiddleware` |
| API authentication | `jwt` | `jwt.MustNew` + `httpx.WithJWT` (or `j.AuthMiddleware`) |
| API rate limiting (limiters) | `ratelimit` | `ratelimit.NewTokenBucket` / `NewRedisTokenBucket`; HTTP middleware `httpx.WithRateLimit` |
| Downstream protection (fail fast via breaker) | `breaker` | `breaker.NewBreaker` or `breaker.Do`; per-route isolation with `httpx.WithRouteBreaker` |
| Failure retries | `retry` | `retry.Do` |
| Async task queue | `taskq` | `taskq.NewProducer` / `NewConsumer` |
| Object storage (local files / OSS/COS/KODO) | `storage` | `storage.New` |
| WebSocket realtime communication | `websocket` | `websocket.MustNew` |
| Distributed tracing (agent / span / propagation) | `trace` | `trace.StartAgent` |
| HTTP tracing middleware | `httpx` | `httpx.WithTracing` |
| Password / digest hashing | `hash` | `hash.BcryptHashDefault` |
| Sensitive-data encryption / request signing | `hash` | `hash.AESGCMEncrypt` / `hash.HMACSign` |
| Type-safe conversion | `cast` | `cast.To[T]` |
| String helpers | `stringx` | `stringx.RandId` |
| General small utilities (path matching / client IP) | `httpx/x` | `x.NewPathMatcher` · `x.ClientIP` (used internally by httpx.With*) |
| Response wrapping (status/bytes/timeout/encryption buffering) | `httpx/respw` | `respw.NewRecorderWriter` · `NewTimeoutWriter` · `NewCryptionWriter` |
| Concurrency primitives | `syncx` | `syncx.NewSingleFlight` |
| Start/stop several services concurrently | `service` | `service.NewServiceGroup` |

**Branching logic (decision points)**:

- Single-host rate limiting → in-memory limiter; shared across instances → Redis limiter; on HTTP always mount with `httpx.WithRateLimit(limiter, ...)`
- HTTP middleware: prefer `httpx.With*` for common capabilities (implementations converge in the `httpx/middleware` subpackage, returning the standard `func(http.Handler) http.Handler`, so gin/echo can reuse them)
- Short-lived temporary data (cache/session/counters) → `redisx`; reliable persistence and transactions → `orm`
- APIs that must respond synchronously → `httpx` (plus `websocket` when needed); async consumption is acceptable → `taskq`
- Mutual exclusion around a critical section → `redisx.Locker` (cross-instance) or `syncx` (single process)
- Preventing cache stampede / duplicate execution within one request → `syncx.NewSingleFlight`
- A unified interface for file upload/download → `storage` (the Driver decides local/OSS/COS/KODO)

## Workflow

### Step 1 — Requirement analysis: decide which modules are needed

1. List every infrastructure requirement of the service (config, logging, storage, HTTP, auth, rate limiting, retries, queues, realtime communication…)
2. Use the decision table to pick one module per requirement
3. Note the ordering dependencies between modules (e.g. jwt depends on the Secret supplied by conf; ServiceGroup depends on each service's start/stop)
4. Lay out a skeleton following the recommended business directory structure (config / svc / route / handler / logic / model / middleware), see [project-structure](./references/project-structure.md)

### Step 2 — Add the dependencies

```bash
go get github.com/chihqiang/infra-go/conf
go get github.com/chihqiang/infra-go/logger
go get github.com/chihqiang/infra-go/httpx
go get github.com/chihqiang/infra-go/orm
go get github.com/chihqiang/infra-go/redisx
go get github.com/chihqiang/infra-go/jwt
go get github.com/chihqiang/infra-go/ratelimit
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
logger.New(logger.Config{Level: logger.InfoLevel, AppName: "my-service"})
defer logger.Sync()

// 2. Database
db := orm.MustNew(orm.Config{Driver: orm.DriverMySQL, Host: cfg.Host, ...})
defer orm.Close(db)

// 3. Redis
client := redisx.MustNew(redisx.Config{Addr: cfg.RedisAddr, KeyPrefix: "myapp"})
defer client.Close()

// 4. JWT (used by middleware)
j := jwt.MustNew(jwt.Config{Secret: cfg.JWTSecret, ...})

// 5. Distributed tracing (optional; then instrument HTTP with httpx.WithTracing)
trace.StartAgent(trace.Config{Name: "my-service", Endpoint: cfg.OtelEndpoint})
defer trace.StopAgent()
```

Convention: use `MustXxx` for global singletons / service-level components (panic if startup fails); use `Xxx` and handle the error explicitly for recoverable local objects; every component holding connections or background goroutines gets a `defer Close/Stop/Sync`.

### Step 5 — Assemble the HTTP server and middleware with httpx

1. Define request structs and validate them with `binding` tags:

```go
type CreateUserRequest struct {
    Name  string `json:"name" binding:"required"`
    Email string `json:"email" binding:"required,email"`
}
```

2. Register routes; `MustBind*` does binding + validation + automatic error response in one step:

```go
server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})
server.AddRoute(httpx.Route{
    Method: "POST", Path: "/users",
    Handler: func(w http.ResponseWriter, r *http.Request) {
        var req CreateUserRequest
        if err := httpx.MustBindJSON(w, r, &req); err != nil {
            return // 400 already written automatically
        }
        httpx.OkJSON(w, createUser(req)) // wrapped into Response[T] automatically: code=0, msg=ok
    },
})
```

3. Mount middleware (request ID, recovery, tracing, rate limiting, JWT auth, etc.):

```go
server.Use(httpx.WithTracing("/healthz"))          // tracing (put first so logs carry trace_id)
server.Use(httpx.WithRequestID())
server.Use(httpx.WithRecovery())
server.Use(httpx.WithCors("*"))
server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200))) // rate limiting: 429
server.Use(httpx.WithJWT(j, func(r *http.Request) string {          // JWT auth
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
```

> General/third-party standard middleware: `server.Use(httpx.AsMiddleware(myStdMiddleware))`. The middleware core logic lives in the `httpx/middleware` subpackage, and you can also use `middleware.NewXxx().Middleware()` directly for reuse with gin/echo/the standard library.

4. Start it (blocking; supports graceful shutdown on SIGINT/SIGTERM/SIGHUP):

```go
server.Start()
```

### Step 6 — Orchestrate the lifecycle of several services with service

```go
sg := service.NewServiceGroup()
sg.Add(service.AsService(server))               // *httpx.Server: AsService adapts Start()/Stop() error
sg.Add(service.WithStart(func() { _ = consumer.Run() })) // taskq consumer: wrap it with WithStart
sg.Start() // blocking; returns once everything has exited; Stop is guaranteed to run only once
```

### Step 7 — Handle errors, context and unified responses the agreed way

- Error messages and comments in English (the project-wide style)
- At the HTTP layer always use `httpx.OkJSON(w, data)` / `httpx.OkJSONCtx(ctx, w, data)` / `httpx.WriteHTTPError(w, status, msg)` / `httpx.WriteHTTPErrorWithCode(w, status, code, msg)`
- When traceID / requestID correlation is needed, use the `Ctx` response variants together with `logger.XxxCtx(ctx, ...)`
- Pass `context.Context` across modules consistently (orm / redisx / retry / taskq all support ctx timeouts and cancellation)
- Inspect semantic errors with `errors.Is`: `redisx.ErrNil`, `jwt.ErrExpiredToken`, `retry.ErrMaxRetries`, etc.

## Acceptance Criteria

- [ ] Only modules that are actually used have been imported
- [ ] Configuration is loaded through `conf`, with defaults and environment variable support; sensitive values come from environment variables
- [ ] All components are initialized in dependency order; connection-holding components have `Close / Stop / Sync`
- [ ] HTTP routes use `MustBind*` for parameters; responses go through `httpx.Ok*` / `WriteHTTPError` consistently
- [ ] Auth / rate limiting / tracing / request ID / recovery are mounted as middleware (`httpx.With*`)
- [ ] Custom standard middleware is integrated via `httpx.AsMiddleware`
- [ ] Multiple services are started and stopped together with `service.NewServiceGroup`, with graceful shutdown
- [ ] Error messages and comments are English, matching the project style
- [ ] `retry` is used wherever network / transient failures occur; Redis rate limiting / locks are used in multi-instance setups
- [ ] `go build ./...` and `go vet ./...` pass

## References

The API documentation for each module is maintained in the `references/` directory (one file per module), covering installation, configuration, initialization, key methods and error conventions. For the HTTP-related subpackages (`binding`/`middleware`/`x`/`respw`), see [httpx](./references/httpx.md) and its links.

| Category | Module docs |
|------|------|
| Configuration and logging | [conf](./references/conf.md) · [logger](./references/logger.md) |
| Data layer | [orm](./references/orm.md) · [redisx](./references/redisx.md) · [cache](./references/cache.md) |
| HTTP and APIs | [httpx](./references/httpx.md) (includes the binding/middleware/x/respw subpackages) · [jwt](./references/jwt.md) · [ratelimit](./references/ratelimit.md) · [breaker](./references/breaker.md) · [retry](./references/retry.md) · [websocket](./references/websocket.md) |
| Async and storage | [taskq](./references/taskq.md) · [storage](./references/storage.md) |
| Observability and security | [trace](./references/trace.md) · [hash](./references/hash.md) |
| General utilities | [cast](./references/cast.md) · [stringx](./references/stringx.md) · [syncx](./references/syncx.md) |
| Service orchestration | [service](./references/service.md) · [mapping](./references/mapping.md) |
| Project structure | [project-structure](./references/project-structure.md) |

> `x` and `respw` have moved into `httpx` as subpackages (`httpx/x`, `httpx/respw`); see the directory tree at the top of [httpx subpackage structure](./references/httpx.md), plus the standalone files [httpx-x](./references/httpx-x.md) and [httpx-respw](./references/httpx-respw.md).

All modules follow the same conventions: `New` returns an error and `MustNew` panics on error; connection-holding components have `Close / Stop / Sync`; comments and errors in English.
