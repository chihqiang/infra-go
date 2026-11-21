<div align="center">
<h1>ten-go</h1>

[![Auth](https://img.shields.io/badge/Auth-chihqiang-ff69b4)](https://github.com/chihqiang)
[![Release](https://img.shields.io/github/release/chihqiang/ten-go.svg?style=flat-square)](https://github.com/chihqiang/ten-go/releases)
[![HitCount](https://views.whatilearened.today/views/github/chihqiang/ten-go.svg)](https://github.com/chihqiang/ten-go)
[![Tests](https://github.com/chihqiang/ten-go/actions/workflows/test.yml/badge.svg)](https://github.com/chihqiang/ten-go/actions/workflows/test.yml)
[![GitHub license](https://img.shields.io/github/license/chihqiang/ten-go)](https://github.com/chihqiang/ten-go/blob/main/LICENSE)

<p>
 ten-go is a Go web framework with batteries included: routing, middleware, auth and WebSocket,
 plus pluggable infrastructure for configuration, logging, database, Redis, cache, storage and
 task queues.
</p>

</div>

## Why ten-go?

`ten` is the number 10, and 10 is written with the only two digits a computer ever uses: `1`
and `0`. That is the whole idea behind this library — services should be assembled from small,
predictable parts rather than one opaque block:

| Symbol | What it stands for here |
|--------|-------------------------|
| `1` | the modules that are switched on: the config, logger, database, Redis, HTTP and WebSocket pieces a service actually needs |
| `0` | the parts that stay out: every module is optional, so unused capabilities add no weight |

The `-go` suffix simply says what it is written in.

## Quick start

A minimal HTTP service example (configuration `conf` + logging `logger` + HTTP `rest` +
lifecycle orchestration `service`) that needs no external services; add `orm` / `redisx` only
when you need a database or Redis:

```go
package main

import (
    "errors"
    "io/fs"
    "net/http"

    "github.com/chihqiang/ten-go/core/conf"
    "github.com/chihqiang/ten-go/core/logger"
    "github.com/chihqiang/ten-go/core/service"
    "github.com/chihqiang/ten-go/rest"
)

// Configuration: json tags declare defaults and constraints; conf can load it from a file or
// from environment variables
type Config struct {
    // rest.Config is embedded: server, logger and middleware settings flatten into
    // the top level of the config file, and its json tags supply the defaults for
    // fields missing from that file
    rest.Config
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
    // 1. Configuration loading: defaults / JSON and YAML / environment variable expansion.
    // conf.Load reports a missing file as an error; treat that case as "use the
    // struct defaults" so the quick start runs without a config file.
    var cfg Config
    if err := conf.Load("config.yaml", &cfg, conf.UseEnv()); err != nil {
        if !errors.Is(err, fs.ErrNotExist) {
            logger.Fatal("load config failed", logger.Err(err))
        }
        if err := conf.FillDefault(&cfg); err != nil {
            logger.Fatal("fill config defaults failed", logger.Err(err))
        }
    }

    // 2. HTTP service: NewServer configures the global logger from cfg.Logger;
    //    recovery + request ID + access log are on by default
    server, err := rest.NewServer(cfg.Config)
    if err != nil {
        logger.Fatal("create server failed", logger.Err(err))
    }

    server.Group("/api").AddRoute(rest.Route{
        Method: http.MethodPost,
        Path:   "/hello",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := rest.Bind(r, &req); err != nil {
                _ = rest.ErrorJSON(w, err) // binding failures map to 400/415 automatically
                return
            }
            _ = rest.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        },
    })

    // 3. Lifecycle orchestration: Register adds the HTTP server, logger sync and
    //    trace agent service (when configured) to the group
    sg := service.NewServiceGroup()
    server.Register(sg)
    sg.Start()
}
```

Together with `config.yaml` (optional in this example, because the code above falls back to the
struct defaults when the file does not exist):

```yaml
http:
  host: 0.0.0.0
  port: 8080
```

## Modules

The documentation of each module is maintained centrally in
[skills/ten-go-usage/references](./skills/ten-go-usage/references), one page per module:

| Module | Description |
| ------ | ------ |
| [conf](./skills/ten-go-usage/references/conf.md) | Configuration parsing with JSON/YAML support, defaults, environment variables and parameter validation |
| [logger](./skills/ten-go-usage/references/logger.md) | Structured logger with JSON/console encoding and rolling log files |
| [orm](./skills/ten-go-usage/references/orm.md) | ORM wrapper built on gorm, supporting MySQL/PostgreSQL/SQLite |
| [redisx](./skills/ten-go-usage/references/redisx.md) | Redis client wrapper: connection pool, health check, distributed lock |
| [cache](./skills/ten-go-usage/references/cache.md) | Unified cache interface with two implementations: in-memory (LRU eviction, hit-rate statistics) and Redis (protection against cache breakdown and penetration) |
| [rest](./skills/ten-go-usage/references/rest.md) | HTTP toolkit: config (`Config`), method-aware routing, route groups, middleware chain, request binding and graceful shutdown. Handlers live in `rest/handler` (recovery, request ID, tracing, access log, concurrency/timeout/body limits, gzip, breaker, rate limit, JWT auth, body signature) as structs with a `Config` and a `Handle` method, so they can be reused outside a `rest.Server`; binding is exposed as `rest.Bind*`, and JSON/XML responses plus the unified `code/msg/data` envelope live in `rest` itself (`OkJSON` / `ErrorJSON` / `OkXML` / `ErrorXML`); CORS comes from `rest.WithCORS` |
| [ratelimit](./skills/ten-go-usage/references/ratelimit.md) | Rate limiters: token bucket/sliding window with in-memory and Redis backends. HTTP integration is `rest/handler.NewRateLimitHandler` |
| [breaker](./skills/ten-go-usage/references/breaker.md) | Circuit breaker using the Google SRE algorithm: fail fast, degrade and prevent cascading failures |
| [retry](./skills/ten-go-usage/references/retry.md) | Retry mechanism: exponential backoff, fixed delay and jitter |
| [hash](./skills/ten-go-usage/references/hash.md) | Hashing and encryption: MD5/SHA/Bcrypt/HMAC, AES-GCM encryption, HMAC signing/verification |
| [trace](./skills/ten-go-usage/references/trace.md) | Tracing built on OpenTelemetry: agent / span management / gRPC and HTTP header propagation / attribute helpers. HTTP server instrumentation is `rest/handler.NewTraceHandler` |
| [mapping](./skills/ten-go-usage/references/mapping.md) | map → struct deserialisation with a struct tag parsing engine |
| [cast](./skills/ten-go-usage/references/cast.md) | Type-safe conversion covering basic types, time, slices and generics |
| [syncx](./skills/ten-go-usage/references/syncx.md) | Concurrency helpers: SingleFlight/ConcurrentMap/Semaphore |
| [service](./skills/ten-go-usage/references/service.md) | Service group that starts and stops several Services concurrently, with sync.Once guaranteeing a single stop |
| [taskq](./skills/ten-go-usage/references/taskq.md) | Asynchronous task queue built on asynq with a producer/consumer pattern |
| [storage](./skills/ten-go-usage/references/storage.md) | Unified object storage interface supporting local files, Alibaba Cloud OSS, Tencent Cloud COS and Qiniu KODO; provides write/read/exists/delete/URL building |
| [ws](./skills/ten-go-usage/references/ws.md) | WebSocket service wrapper built on gorilla/websocket: event driven, room broadcast and heartbeat detection |
| [stringx](./skills/ten-go-usage/references/stringx.md) | String toolkit with common helpers for random generation, predicates, conversion, splitting and joining |

## Features

- **Consistent style**: all modules use English comments, English error messages and
  functional option configuration
- **Controlled dependencies**: packages are imported independently, so unused packages are not
  compiled into the binary. The lightweight toolkits (`cast` / `stringx` / `syncx` / `retry` /
  `mapping`) have zero third-party dependencies; the HTTP stack (`rest`) does depend on the
  tracing, JWT, validation and rate-limiting packages it integrates with (OpenTelemetry, gRPC,
  go-redis), so a `rest` service pulls those into its build graph
- **Type safety**: generics are used extensively (`BaseResponse[T]`, `cast.To[T]`)
- **Testable**: every module has complete unit tests and supports `-race` detection
- **Dependency governance**: the dependency baseline is maintained centrally and upgraded
  regularly (currently Go 1.25 / gorm 1.31 / OpenTelemetry 1.44 and so on)

## Skill installation

The repository ships a VS Code Copilot skill - [ten-go-usage](./skills/ten-go-usage/SKILL.md) -
which guides you through choosing and assembling ten-go modules (configuration, logging,
database, Redis, HTTP, JWT, ...) in a business project.

The skill lives in `skills/ten-go-usage/` on `main` (`SKILL.md` plus one reference page per
module), so it travels with the library and is discovered automatically by the
[Agent Skills CLI](https://github.com/vercel-labs/skills) (`npx skills`). Run the installation
from the project that should use the skill, not from this repository:

```bash
# Preview the skills discovered in this repository (nothing is installed)
npx skills add chihqiang/ten-go --list

# Install into the current project -> .agents/skills/ten-go-usage (+ skills-lock.json)
npx skills add chihqiang/ten-go --skill ten-go-usage -a github-copilot

# Install once for all projects -> ~/.agents/skills/ten-go-usage
npx skills add chihqiang/ten-go --skill ten-go-usage -g -a github-copilot

# Offline / from a local clone of this repository
npx skills add ./ten-go/skills/ten-go-usage -g -a github-copilot
```

The CLI copies the whole skill directory and prints the destination it used, so re-run the same
command after upgrading; `npx skills update` refreshes installed skills and
`npx skills remove ten-go-usage` uninstalls one.

### Where VS Code looks

For the Copilot harness, VS Code reads project skills from `.github/skills/` and personal skills
from `~/.copilot/skills/`; local sessions additionally read `.agents/skills/` and
`~/.agents/skills/`, which is where `npx skills` installs by default. If the skill is not listed
under **Chat: Open Customizations → Skills**, link it into a directory your harness reads:

```bash
mkdir -p ~/.copilot/skills
ln -s ~/.agents/skills/ten-go-usage ~/.copilot/skills/ten-go-usage
```

### Usage

Once installed, type `/ten-go-usage` in the VS Code chat (or just ask a question, such as
"build an HTTP service with login authentication and rate limiting using ten-go") and Copilot
loads the skill automatically to help you follow its workflow.

## Quality checks

```bash
go build ./...                # build every package
go vet ./...                  # static analysis
go test ./... -race -count=1  # full unit tests + race detection
```

## Acknowledgements

ten-go stands on the shoulders of two excellent Go frameworks. The shape of the `rest` package
- especially the server/engine split, the `chain` + `handler` middleware layout, route groups and
the `config → server → route → handler → logic` layering - follows
[go-zero](https://github.com/zeromicro/go-zero)'s `rest` design, and the request binding layer
(content-type driven selection, tag mapping, `binding` validation tags) follows
[gin](https://github.com/gin-gonic/gin)'s `binding` package.

Both are MIT licensed; ten-go carries an independent implementation, but the architecture is
clearly theirs:

- **go-zero** — `rest`, `rest/chain`, `rest/handler`, `rest/router`, `rest/token` and the
  middleware naming (`RecoveryHandler`, `MaxBytesHandler`, `TimeoutHandler`, ...)
- **gin** — `rest/internal/binding`: `Binding`/`Default`, form/query/header mapping and
  validator integration

Thanks to the go-zero and gin maintainers and contributors for publishing designs worth learning
from.

## License

Released under the [Apache License 2.0](./LICENSE), Copyright 2026 chihqiang.

You may use, modify and redistribute ten-go - including in commercial and closed-source
projects - provided the copyright and license notices are preserved. The complete terms,
including the patent grant and the redistribution requirements, are in the
[LICENSE](./LICENSE) file.
