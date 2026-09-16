# infra-go

A general-purpose infrastructure wrapper library for Go projects, bundling storage, logging,
configuration and utility capabilities.

> **Requirements**: Go 1.25+ (`go.mod` declares `go 1.25.11`)

## Quick start

A minimal HTTP service example (configuration `conf` + logging `logger` + HTTP `httpx` +
lifecycle orchestration `service`) that needs no external services; add `orm` / `redisx` only
when you need a database or Redis (see [Dependency footprint](#dependency-footprint) below for
the dependency scope):

```go
package main

import (
    "net/http"
    "github.com/chihqiang/infra-go/conf"
    "github.com/chihqiang/infra-go/httpx"
    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/service"
)

// Configuration: json tags declare defaults and constraints; conf can load it from a file or
// from environment variables
type Config struct {
    Host string `json:",default=0.0.0.0"`
    Port int    `json:",default=8080,range=[1:65535]"`
}

// Request body: the binding tag performs parameter validation
type helloRequest struct {
    Name string `json:"name" binding:"required"`
}

func main() {
    // 1. Global logging (initialised first); Sync flushes the buffer before exit
    logger.SetGlobal(logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "demo",
    }))
    defer logger.Sync()

    // 2. Configuration loading: defaults / JSON and YAML / environment variable expansion
    var cfg Config
    if err := conf.Load("config.yaml", &cfg, conf.UseEnv()); err != nil {
        logger.Fatal("load config failed", logger.Err(err))
    }

    // 3. HTTP service: parameter binding + validation + unified response, pluggable middleware
    srv := httpx.NewServer(httpx.ServerConfig{Host: cfg.Host, Port: cfg.Port})
    // More middleware lives in the httpx module: rate limit, tracing, JWT, ...
    srv.Use(httpx.WithRequestID(), httpx.WithRecovery())
    srv.AddRoute(httpx.Route{
        Method: "POST",
        Path:   "/hello",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := httpx.MustBindJSON(w, r, &req); err != nil {
                return // a binding / validation failure has already written 400
            }
            httpx.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        },
    })

    // 4. Lifecycle orchestration: service.AsService adapts *httpx.Server into a Service,
    //    starting and stopping everything concurrently
    sg := service.NewServiceGroup()
    sg.Add(service.AsService(srv))
    sg.Start()
}
```

Together with `config.yaml` (optional; the struct defaults are used when it is absent):

```yaml
host: 0.0.0.0
port: 8080
```

## Modules

The documentation of each module is maintained centrally in
[skills/infra-go-usage/references](./skills/infra-go-usage/references), one page per module:

| Module | Description |
| ------ | ------ |
| [conf](./skills/infra-go-usage/references/conf.md) | Configuration parsing with JSON/YAML support, defaults, environment variables and parameter validation |
| [logger](./skills/infra-go-usage/references/logger.md) | Logging wrapper built on zap + lumberjack with rolling log files |
| [orm](./skills/infra-go-usage/references/orm.md) | ORM wrapper built on gorm, supporting MySQL/PostgreSQL/SQLite (all three drivers are compiled into this package, see "Dependency footprint") |
| [redisx](./skills/infra-go-usage/references/redisx.md) | Redis client wrapper: connection pool, health check, distributed lock |
| [cache](./skills/infra-go-usage/references/cache.md) | Unified cache interface with two implementations: in-memory (LRU eviction, hit-rate statistics) and Redis (protection against cache breakdown and penetration) |
| [httpx](./skills/infra-go-usage/references/httpx.md) | HTTP toolkit: request parameter binding, unified generic responses, route registration and graceful shutdown. Built-in middleware: CORS/Recovery/RequestID/tracing/access log/circuit breaker/timeout/request body limit/gzip/concurrency limit/rate limit/JWT auth/encryption/content safety. The core is split into subpackages: `binding` (binding implementation), `middleware` (generic middleware with the standard `func(http.Handler) http.Handler` shape, reusable from gin/echo), `x` (shared helpers: path matching + client IP parsing), `respw` (ResponseWriter wrapper) |
| [ratelimit](./skills/infra-go-usage/references/ratelimit.md) | Rate limiters: token bucket/sliding window with in-memory and Redis backends. The HTTP rate limit middleware is unified as `httpx.WithRateLimit` (implemented in the `httpx/middleware` subpackage) |
| [breaker](./skills/infra-go-usage/references/breaker.md) | Circuit breaker using the Google SRE algorithm: fail fast, degrade and prevent cascading failures |
| [retry](./skills/infra-go-usage/references/retry.md) | Retry mechanism: exponential backoff, fixed delay and jitter |
| [jwt](./skills/infra-go-usage/references/jwt.md) | JWT signing and parsing with HS256/HS384/HS512 (HMAC) support; the authentication middleware `AuthMiddleware` / `httpx.WithJWT` injects the business claims into the context after validation |
| [hash](./skills/infra-go-usage/references/hash.md) | Hashing and encryption: MD5/SHA/Bcrypt/HMAC, AES-GCM encryption, HMAC signing/verification |
| [trace](./skills/infra-go-usage/references/trace.md) | Tracing built on OpenTelemetry: agent / span management / gRPC and HTTP header propagation / attribute helpers. HTTP server instrumentation is unified as `httpx.WithTracing` (all four exporters are compiled into this package, see "Dependency footprint") |
| [mapping](./skills/infra-go-usage/references/mapping.md) | map → struct deserialisation with a struct tag parsing engine |
| [cast](./skills/infra-go-usage/references/cast.md) | Type-safe conversion covering basic types, time, slices and generics |
| [syncx](./skills/infra-go-usage/references/syncx.md) | Concurrency helpers: SingleFlight/ConcurrentMap/Semaphore |
| [service](./skills/infra-go-usage/references/service.md) | Service group that starts and stops several Services concurrently, with sync.Once guaranteeing a single stop |
| [taskq](./skills/infra-go-usage/references/taskq.md) | Asynchronous task queue built on asynq with a producer/consumer pattern |
| [storage](./skills/infra-go-usage/references/storage.md) | Unified object storage interface supporting local files, Alibaba Cloud OSS, Tencent Cloud COS and Qiniu KODO; provides write/read/exists/delete/URL building (all three SDKs are compiled into this package, see "Dependency footprint"). |
| [websocket](./skills/infra-go-usage/references/websocket.md) | WebSocket service wrapper built on gorilla/websocket: event driven, room broadcast and heartbeat detection |
| [stringx](./skills/infra-go-usage/references/stringx.md) | String toolkit with common helpers for random generation, predicates, conversion, splitting and joining |

## Features

- **Consistent style**: all modules use English comments, English error messages and
  functional option configuration
- **Controlled dependencies**: modules are imported independently, and modules you do not use
  **never** enter your `go.mod` / `go.sum` (Go 1.17+ module graph pruning). The lightweight
  toolkits (`cast` / `stringx` / `syncx` / `retry` / `mapping`) have zero third-party
  dependencies; but when sibling implementations are packed into one package you cannot pick
  just one of them, see "Dependency footprint" below
- **Type safety**: generics are used extensively (`Response[T]`, `cast.To[T]`)
- **Testable**: every module has complete unit tests and supports `-race` detection
- **Dependency governance**: the dependency baseline is maintained centrally and upgraded
  regularly (currently Go 1.25 / gorm 1.31 / OpenTelemetry 1.44 and so on)

### Dependency footprint

Module graph pruning guarantees that **modules you do not import never enter the consumer
build**: when you only import `stringx`, for example, the consumer's
`go.mod` contains a single require for infra-go itself, `go.sum` has just 8 lines, and the
resulting binary contains no cloud SDK / gorm / asynq symbols.

However, **sibling implementations inside the same package are compiled in together**, so you
cannot pick just one of them. The number of third-party modules each module actually pulls in:

| Module | Third-party modules | Description |
| ------ | ------------------- | ----------- |
| `cast` `mapping` `retry` `stringx` `syncx` | 0 | standard library only |
| `conf` `hash` | 1 | yaml.v3 / x/crypto |
| `breaker` `logger` `ratelimit` `service` | 3 | |
| `cache` `redisx` | 6 | go-redis |
| `websocket` | 7 | |
| `jwt` | 11 | |
| `storage` | 12 | Alibaba Cloud OSS + Tencent Cloud COS + Qiniu KODO SDKs, **all of them** |
| `taskq` | 12 | asynq + go-redis + cron |
| `orm` | 14 | all three MySQL + PostgreSQL + SQLite drivers, **in full** (including `mattn/go-sqlite3`) |
| `httpx` | 16 | |
| `trace` | 17 | all four exporters: OTLP gRPC / OTLP HTTP / stdout / Zipkin, **in full** |

The quick-start example (`conf` + `logger` + `httpx` + `service`) actually pulls in 17
third-party modules, but **excludes** cloud vendor SDKs,
gorm drivers, asynq and OTel exporters.

> ⚠️ **`storage` / `trace` / `orm` currently cannot pull in a single implementation only**:
> importing `orm` alone (and using MySQL only) still compiles in the postgres and sqlite
> drivers, which grows the consumer's `go.sum` from 8 lines to 56 and the binary from 2.5M to
> 7.5M.
>
> - It still builds with `CGO_ENABLED=0` (`mattn/go-sqlite3` ships a non-cgo stub), but sqlite
>   is unusable at runtime.
> - If the artefact size matters, bypass the wrapper and import `gorm.io/driver/*` or
>   `go.opentelemetry.io/otel/exporters/*` directly, or use the cloud vendor's official SDK.

## Skill installation

The repository ships a VS Code Copilot skill - [infra-go-usage](./skills/infra-go-usage/SKILL.md) -
which guides you through choosing and assembling infra-go modules (configuration, logging,
database, Redis, HTTP, JWT, ...) in a business project.

Install it in one step with the [Agent Skills CLI](https://github.com/vercel-labs/skills)
(`npx skills`); the skill lives in `skills/infra-go-usage/` in this repository and has been
pushed to the remote `main` branch:

```bash
# Install into the current project (defaults to .claude/skills/ or .agents/skills/,
# auto-detecting the installed agent)
npx skills add chihqiang/infra-go --skill infra-go-usage

# Preview which skills can be discovered in the repository (without installing)
npx skills add chihqiang/infra-go --list

# Install into the personal directory (usable across projects) and specify the target agent
npx skills add chihqiang/infra-go --skill infra-go-usage -g -a github-copilot
```

### Usage

Once installed, type `/infra-go-usage` in the VS Code chat (or just ask a question, such as
"build an HTTP service with login authentication and rate limiting using infra-go") and Copilot
loads the skill automatically to help you follow its workflow.

> Note: this repository keeps the skill in the root `skills/` directory so that it travels with
> the library; VS Code recognises project-level skills in `.github/skills/`, `.agents/skills/`
> or `.claude/skills/`.

## Quality checks

```bash
go build ./...                # build every package
go vet ./...                  # static analysis
go test ./... -race -count=1  # full unit tests + race detection
```

## License

[Apache-2.0](./LICENSE)
