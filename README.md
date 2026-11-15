<div align="center">
<h1>infra-go</h1>

[![Auth](https://img.shields.io/badge/Auth-chihqiang-ff69b4)](https://github.com/chihqiang)
[![GitHub Pull Requests](https://img.shields.io/github/issues-pr/chihqiang/infra-go)](https://github.com/chihqiang/infra-go/pulls)
[![Release](https://img.shields.io/github/release/chihqiang/infra-go.svg?style=flat-square)](https://github.com/chihqiang/infra-go/releases)
[![GitHub Pull Requests](https://img.shields.io/github/stars/chihqiang/infra-go)](https://github.com/chihqiang/infra-go/stargazers)
[![HitCount](https://views.whatilearened.today/views/github/chihqiang/infra-go.svg)](https://github.com/chihqiang/infra-go)
[![GitHub license](https://img.shields.io/github/license/chihqiang/infra-go)](https://github.com/chihqiang/infra-go/blob/main/LICENSE)

<p>
 infra-go is an efficient Go infrastructure wrapper library for building services, bundling
 configuration, logging, database, Redis, HTTP, object storage and utility capabilities that
 are pluggable, independently importable and production-ready.
</p>

</div>

## Quick start

A minimal HTTP service example (configuration `conf` + logging `logger` + HTTP `rest` +
lifecycle orchestration `service`) that needs no external services; add `orm` / `redisx` only
when you need a database or Redis:

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/core/conf"
    "github.com/chihqiang/infra-go/core/logger"
    "github.com/chihqiang/infra-go/core/service"
    "github.com/chihqiang/infra-go/rest"
)

// Configuration: json tags declare defaults and constraints; conf can load it from a file or
// from environment variables
type Config struct {
    // Server settings (host/port/timeouts/middleware switches) live in rest.Config;
    // its json tags supply the defaults used when config.yaml is absent
    HTTP rest.Config `json:"http"`
}

// Request body: implement Validate to run checks after decoding
type helloRequest struct {
    Name string `json:"name"`
}

func (r *helloRequest) Validate() error {
    if r.Name == "" {
        return rest.ErrBadRequest
    }
    return nil
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

    // 3. HTTP service: recovery + request ID + access log are on by default
    server, err := rest.NewServer(cfg.HTTP)
    if err != nil {
        logger.Fatal("create server failed", logger.Err(err))
    }

    server.Group("/api").AddRoute(rest.Route{
        Method: http.MethodPost,
        Path:   "/hello",
        Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := rest.Bind(r, &req); err != nil {
                _ = rest.Error(w, err) // binding failures map to 400/415 automatically
                return
            }
            _ = rest.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        }),
    })

    // 4. Lifecycle orchestration: service.AsService adapts *rest.Server into a Service,
    //    starting and stopping everything concurrently
    sg := service.NewServiceGroup()
    sg.Add(service.AsService(server))
    sg.Start()
}
```

Together with `config.yaml` (optional; the struct defaults are used when it is absent):

```yaml
http:
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
| [orm](./skills/infra-go-usage/references/orm.md) | ORM wrapper built on gorm, supporting MySQL/PostgreSQL/SQLite |
| [redisx](./skills/infra-go-usage/references/redisx.md) | Redis client wrapper: connection pool, health check, distributed lock |
| [cache](./skills/infra-go-usage/references/cache.md) | Unified cache interface with two implementations: in-memory (LRU eviction, hit-rate statistics) and Redis (protection against cache breakdown and penetration) |
| [rest](./skills/infra-go-usage/references/rest.md) | HTTP toolkit: config (`Config`), method-aware routing, route groups, middleware chain, request binding and graceful shutdown. Handlers live in `rest/handler` (recovery, request ID, tracing, access log, concurrency/timeout/body limits, gzip, breaker, rate limit, JWT auth, body signature) as `chain.Middleware`, so they can be reused outside a `rest.Server`; binding is exposed as `rest.Bind*`, and JSON/XML responses plus the `code/msg/data` envelope live in `rest` itself (`OkJSON` / `JSONBaseResponse` / `Error`); CORS comes from `rest.WithCors` |
| [ratelimit](./skills/infra-go-usage/references/ratelimit.md) | Rate limiters: token bucket/sliding window with in-memory and Redis backends. HTTP integration is `rest/handler.RateLimitHandler` |
| [breaker](./skills/infra-go-usage/references/breaker.md) | Circuit breaker using the Google SRE algorithm: fail fast, degrade and prevent cascading failures |
| [retry](./skills/infra-go-usage/references/retry.md) | Retry mechanism: exponential backoff, fixed delay and jitter |
| [hash](./skills/infra-go-usage/references/hash.md) | Hashing and encryption: MD5/SHA/Bcrypt/HMAC, AES-GCM encryption, HMAC signing/verification |
| [trace](./skills/infra-go-usage/references/trace.md) | Tracing built on OpenTelemetry: agent / span management / gRPC and HTTP header propagation / attribute helpers. HTTP server instrumentation is `rest/handler.TraceHandler` |
| [mapping](./skills/infra-go-usage/references/mapping.md) | map → struct deserialisation with a struct tag parsing engine |
| [cast](./skills/infra-go-usage/references/cast.md) | Type-safe conversion covering basic types, time, slices and generics |
| [syncx](./skills/infra-go-usage/references/syncx.md) | Concurrency helpers: SingleFlight/ConcurrentMap/Semaphore |
| [service](./skills/infra-go-usage/references/service.md) | Service group that starts and stops several Services concurrently, with sync.Once guaranteeing a single stop |
| [taskq](./skills/infra-go-usage/references/taskq.md) | Asynchronous task queue built on asynq with a producer/consumer pattern |
| [storage](./skills/infra-go-usage/references/storage.md) | Unified object storage interface supporting local files, Alibaba Cloud OSS, Tencent Cloud COS and Qiniu KODO; provides write/read/exists/delete/URL building |
| [ws](./skills/infra-go-usage/references/ws.md) | WebSocket service wrapper built on gorilla/websocket: event driven, room broadcast and heartbeat detection |
| [stringx](./skills/infra-go-usage/references/stringx.md) | String toolkit with common helpers for random generation, predicates, conversion, splitting and joining |

## Features

- **Consistent style**: all modules use English comments, English error messages and
  functional option configuration
- **Controlled dependencies**: modules are imported independently, and modules you do not use
  **never** enter your `go.mod` / `go.sum` (Go 1.17+ module graph pruning). The lightweight
  toolkits (`cast` / `stringx` / `syncx` / `retry` / `mapping`) have zero third-party
  dependencies
- **Type safety**: generics are used extensively (`BaseResponse[T]`, `cast.To[T]`)
- **Testable**: every module has complete unit tests and supports `-race` detection
- **Dependency governance**: the dependency baseline is maintained centrally and upgraded
  regularly (currently Go 1.25 / gorm 1.31 / OpenTelemetry 1.44 and so on)

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

## Acknowledgements

infra-go stands on the shoulders of two excellent Go frameworks. The shape of the `rest` package
- especially the server/engine split, the `chain` + `handler` middleware layout, route groups and
the `config → server → route → handler → logic` layering - follows
[go-zero](https://github.com/zeromicro/go-zero)'s `rest` design, and the request binding layer
(content-type driven selection, tag mapping, `binding` validation tags) follows
[gin](https://github.com/gin-gonic/gin)'s `binding` package.

Both are MIT licensed; infra-go carries an independent implementation, but the architecture is
clearly theirs:

- **go-zero** — `rest`, `rest/chain`, `rest/handler`, `rest/router`, `rest/token` and the
  middleware naming (`RecoverHandler`, `MaxBytesHandler`, `TimeoutHandler`, ...)
- **gin** — `rest/internal/binding`: `Binding`/`Default`, form/query/header mapping and
  validator integration

Thanks to the go-zero and gin maintainers and contributors for publishing designs worth learning
from.

## License

Released under the [Apache License 2.0](./LICENSE), Copyright 2026 chihqiang.

You may use, modify and redistribute infra-go - including in commercial and closed-source
projects - provided the copyright and license notices are preserved. The complete terms,
including the patent grant and the redistribution requirements, are in the
[LICENSE](./LICENSE) file.
