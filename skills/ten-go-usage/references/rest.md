# rest

HTTP server toolkit: configuration, routing, middleware chain, request binding, response
helpers, JWT based authentication and a set of ready-to-use handlers.

## Features

- **Routing**: Go 1.22 method-aware patterns (`GET /users/{id}`), nested groups, group middleware
- **Middleware chain**: recovery, request ID, tracing, access log, concurrency limit, timeout,
  body limit, gzip, circuit breaker, rate limit, JWT auth, CORS
- **Binding**: JSON / XML / form / multipart / query / header / path with self-validation
- **Responses**: one `BaseResponse` (`code`/`msg`/`data`/`request_id`) envelope for success and errors,
  JSON/XML/HTML writers, error-to-status mapping
- **Lifecycle**: `Start` / `Serve` / `Shutdown` / `Stop`, with `Register` for `service.ServiceGroup`

## Installation

```bash
go get github.com/chihqiang/ten-go/rest
```

## Quick start

```go
package main

import (
    "net/http"

    "github.com/chihqiang/ten-go/rest"
)

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
    server, err := rest.NewServer(rest.DefaultConfig())
    if err != nil {
        panic(err)
    }

    server.Group("/api").AddRoute(rest.Route{
        Method: http.MethodPost,
        Path:   "/hello",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := rest.Bind(r, &req); err != nil {
                _ = rest.ErrorJSON(w, err) // binding failures map to 400/415
                return
            }
            _ = rest.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        },
    })

    if err := server.Start(); err != nil {
        panic(err)
    }
}
```

`rest.DefaultConfig()` fills host/port, timeouts, the body limit and the default middleware
configs. Passing the zero `Config` to `NewServer` uses `DefaultConfig()`; a partially populated
`Config` is used literally, so start from `DefaultConfig()` when customising and want the other
defaults.

The package name `rest` does not collide with `net/http`, so handlers can import both directly.

## Configuration

```go
type Config struct {
    Host            string        `json:",default=0.0.0.0"`
    Port            int           `json:",default=8080,range=[0:65535]"`
    CertFile        string        `json:",optional"`
    KeyFile         string        `json:",optional"`
    ReadTimeout     time.Duration `json:",default=30s"`
    WriteTimeout    time.Duration `json:",default=30s"`
    IdleTimeout     time.Duration `json:",default=60s"`
    ShutdownTimeout time.Duration `json:",default=30s"`
    MaxHeaderBytes  int           `json:",default=5242880"`
    Logger          logger.Config
    Middlewares     MiddlewaresConf
    Health          HealthConfig
}

type HealthConfig struct {
    Enabled bool   `json:",optional"`          // register the endpoint, default false
    Path    string `json:",optional"`          // static path; "/health" when Enabled and empty
}

type MiddlewaresConf struct {
    Recovery  resthandler.RecoveryConfig
    RequestID resthandler.RequestIDConfig
    Trace     resthandler.TraceConfig
    Log       resthandler.LogConfig
    MaxConns  resthandler.MaxConnsConfig
    Timeout   resthandler.TimeoutConfig
    Gunzip    resthandler.GunzipConfig
    MaxBytes  resthandler.MaxBytesConfig
}
```

### Health endpoint

The built-in health route is disabled by default, so an application that already exposes
`/health` is unaffected. Enable it for liveness/readiness probes:

```go
server, err := rest.NewServer(rest.Config{
    Health: rest.HealthConfig{Enabled: true, Path: "/healthz"}, // empty Path becomes /health
})
```

It registers `GET <Path>` (HEAD is served by the same pattern) and responds with
`200 {"code":0,"msg":"ok","data":{"status":"ok"}}`. The path must be static: `/`, a trailing slash, whitespace, query strings
and wildcards are rejected with `rest.ErrInvalidHealthPath`, because `/` and a trailing slash turn
the ServeMux pattern into a subtree that shadows other handlers. Do not register the same path
again yourself; `http.ServeMux` panics on duplicate patterns.

In a business service, embed this struct in the application config instead of giving it its own
field; `core/conf` / `core/mapping` flatten embedded structs, so the server keys stay at the top
level of the config file and the embedded value goes straight to `NewServer`:

```go
type Config struct {
    rest.Config // flatten: host, port, timeouts, logger, middlewares
    App App        `json:"app"`
    DB  orm.Config `json:"db"`
}

server, err := rest.NewServer(cfg.Config)
```

`Logger` configures the global logger at server startup. When any field is set, `NewServer`
builds it and calls `logger.SetGlobal`; the logger sync service added by `NewServer` flushes it
when the `Server.Register` group stops. The zero `logger.Config` leaves the application's global logger untouched.

`Config.Name` names the service once. It fills `Logger.ServiceName` (the `app` field and
the volume log directory) and the trace agent plus span name (`trace.Config.Name`) when those
sections leave them unset; an explicit value in a section always wins, and setting it never
enables the logger or tracing on its own.

Use `rest.WithLogger(l)` to inject an already-built logger instead. It takes precedence over
`Config.Logger`, is used by the access-log middleware, and is deliberately not installed globally
(call `logger.SetGlobal` yourself if the package-level logger should match).

```go
server, _ := rest.NewServer(rest.Config{
    Name:   "api", // fills logger + trace; no need to repeat it per middleware
    Logger: logger.Config{
        Mode:       "file",
        Path:       "/var/log/api",
        Level:      "info",
        Rotation:   "size",
        MaxSize:    100,
        MaxBackups: 7,
        KeepDays:   30,
    },
})
```

Each middleware has its own config struct with an `Enabled` switch and only the fields that
handler uses, for example:

```go
Middlewares: rest.MiddlewaresConf{
    Recovery:  resthandler.RecoveryConfig{Enabled: true},
    RequestID: resthandler.RequestIDConfig{Enabled: true},
    Log: resthandler.LogConfig{
        Enabled:   true,
        SkipPaths: []string{"/healthz"},
    },
    Trace: resthandler.TraceConfig{
        Enabled:     true,
        IgnorePaths: []string{"/health*"},
        Config:      trace.Config{Endpoint: "localhost:4317"}, // name comes from Config.Name
    },
    MaxBytes: resthandler.MaxBytesConfig{Enabled: true, Limit: 5 << 20},
},
```

`Auth`, `RateLimit` and `Breaker` are intentionally not part of
`MiddlewaresConf`. Auth can be scaled from a single route up to the whole server:
`WithRouteAuth` protects one route, group middleware covers a subtree, and
`WithAuth` opts in server-wide. Rate limiting and breakers can be explicitly
mounted server-wide with `WithRateLimit` / `WithBreaker`, or scoped with their handlers via
`Group.Use` / `WithRouteMiddleware`.

`handler.AuthConfig` is YAML-loadable and `NewAuthHandler` builds the parser from its secret, so
the whole section can come from the config file:

```go
type Config struct {
    rest.Config
    Auth resthandler.AuthConfig `json:"auth"`
}

var cfg Config
conf.MustLoad("config.yaml", &cfg, conf.UseEnv())
server, _ := rest.NewServer(cfg.Config,
    rest.WithMiddleware(resthandler.NewAuthHandler(cfg.Auth).Handle))
```

`WithAuth(secret, opts...)` / `WithRouteAuth(secret, opts...)` are the shorthand when only the
secret and a few options are needed:

```go
server, _ := rest.NewServer(cfg.Config, rest.WithAuth(cfg.Auth.Secret))
```

```yaml
auth:
  secret: ${JWT_SECRET}
  skipPaths: ["/health", "/public/*"]
```

The default extractor is `handler.BearerToken` (RFC 6750 `Authorization: Bearer <token>`). When
the token arrives elsewhere, set `AuthConfig.TokenExtractor` or pass `WithTokenExtractor`; the
extractor returns the raw token, and any non-nil error rejects the request with 401
(`token.ErrMissingToken` is the conventional "no credentials" value):

```go
server, _ := rest.NewServer(cfg.Config, rest.WithAuth(cfg.Auth.Secret,
    resthandler.WithTokenExtractor(func(r *http.Request) (string, error) {
        if value := r.URL.Query().Get("access_token"); value != "" {
            return value, nil
        }
        return "", token.ErrMissingToken
    }),
))
```

`handler.AuthOption` values apply on top of the config, so they work with `WithAuth` and
`NewAuthHandler` alike:

| Auth option | Effect |
|-------------|--------|
| `WithPrevSecret(secret)` | Also accept tokens signed with the previous secret (key rotation) |
| `WithAuthSkipPaths(paths...)` | Paths that bypass auth (exact, trailing-`*` prefix or glob) |
| `WithUnauthorizedCallback(fn)` | Custom rejection body/headers; the default status applies when unset |
| `WithTokenExtractor(fn)` | Replace the default `BearerToken` extractor to read tokens from a query parameter, cookie or custom header (`nil` restores the default) |

`skipPaths` (and the access-log/trace equivalents) are matched against the cleaned request path:
`.`/`..` segments and duplicate slashes are removed first, matching ServeMux routing. A request
such as `/public/../private` therefore cannot widen a `/public/*` rule.

Tags are consumed by `core/conf` and `core/mapping`, so the same struct works for YAML/JSON files
and environment expansion. Every config field carries an explicit key and `logger`, `middlewares`
and `health` are all `optional`: leave a section out and it keeps its zero value. The nested
`default=` tags apply as soon as the section exists, and each `middlewares` entry you do not list
stays disabled. Omitting `logger` therefore means `NewServer` builds no server-owned logger and
leaves the global logger untouched.

```yaml
logger:
  serviceName: api
  mode: file
  path: /var/log/api
  level: info

health:
  enabled: true
  path: /healthz

middlewares:
  recovery:
    enabled: true
  requestID:
    enabled: true
  log:
    enabled: true
    skipPaths: ["/healthz"]
  timeout:
    enabled: true
    timeout: 5s
  maxBytes:
    enabled: true
    limit: 5242880
```

`middlewares` entries are typed as structs and tagged `optional` (`json:",optional"`, the key is the
field name): an
entry that is not listed keeps its zero value, so middleware you rely on must appear in the file.
The handler's own `default=` tags apply as soon as the entry is present, so `timeout: {}` means
"enabled with defaults". A section key written with nothing under it (`middlewares:` while every
entry is commented out) is treated like an omitted section instead of failing as a nil value.
`Health.Path` defaults to `/health` through its tag, so `health: {enabled: true}` is enough;
`internal/healthpath` additionally accepts a relative path such as `healthz` and keeps `/health`
as the fallback for configs built in code that leave the path empty.

`Config.Setup()` applies `DefaultConfig` to a zero `Config`, propagates `Config.Name` and then
validates, returning the configuration `NewServer` will use; call it when application code needs
the normalized values. `Config.Validate()` alone rejects an out-of-range port, half-configured TLS
and negative server timeouts. `Config.WithName()` performs just the name propagation (logger
`ServiceName` and trace `Name`). Configured middleware settings (negative limits) are only checked
while that middleware is enabled, so disabled features do not force their configuration to be
valid.

The access log and middleware error logs resolve the client IP with
`realip.FromRequest`: the first `X-Forwarded-For` entry, then `X-Real-IP`, then the direct peer,
with ports stripped. The forwarding headers are trusted from any peer because this value is only
used for logging; use `rest.ClientIP` (direct peer) or `rest.ClientIPTrusted` with
`rest.ParseTrustedProxies` when application code needs a spoof-resistant address.

The server's configured middleware execution order is fixed:

```text
Recover → RequestID → Trace → Log → MaxConns → Timeout → Gunzip → MaxBytes → Use(...) → route
```

Authentication, rate limits and breakers are only applied where explicitly mounted.

`resthandler.NewCORSHandler` runs first in the chain, so 404/405 responses and responses generated by other middleware carry CORS headers too.

## Server

| Method | Purpose |
|--------|---------|
| `NewServer(conf Config, opts ...Option) (*Server, error)` | Build a server, validating config |
| `Handle(method, path string, h http.HandlerFunc)` / `HandleFunc` | Register a single route |
| `AddRoute(Route, ...RouteOption)` / `AddRoutes(...Route)` | Register routes with options |
| `Group(prefix string) *RouteGroup` | Route group, embeddable/nestable, with `Use` |
| `Use(chain.Middleware)` | Append a server-wide middleware |
| `SetNotFoundHandler` / `SetNotAllowedHandler` | Custom 404 / 405 handlers (`WithNotFoundHandler` / `WithNotAllowedHandler` are the option forms) |
| `Start()` | Listen on the configured address (TLS when cert+key are set) |
| `Serve(net.Listener)` | Serve a caller-provided listener (supports port `:0` tests) |
| `Shutdown(ctx)` / `Stop()` / `Close()` | Graceful / graceful-with-timeout / immediate shutdown |
| `AddService(service.Service)` | Add an auxiliary lifecycle service (the configured trace agent is added automatically) |
| `Register(*service.ServiceGroup)` | Add the HTTP server and all auxiliary services to a group |
| `Addr()`, `Router()`, `ServeHTTP` | Introspection and `httptest` usage |

```go
server, _ := rest.NewServer(rest.Config{
    Host: "127.0.0.1",
    Port: 0,
    Middlewares: rest.MiddlewaresConf{
        Recovery:  resthandler.RecoveryConfig{Enabled: true},
        RequestID: resthandler.RequestIDConfig{Enabled: true},
    },
})

api := server.Group("/api")          // "/api"
v1 := api.Group("/v1")               // "/api/v1"
v1.Use(resthandler.NewTimeoutHandler(resthandler.TimeoutConfig{
    Enabled: true,
    Timeout: time.Second,
}).Handle)
v1.HandleFunc(http.MethodGet, "/users/{id}", handler)
```

Group middleware applies to routes registered after `Use` and is inherited by child groups.

## Server options

```go
server, _ := rest.NewServer(conf,
    rest.WithCORS("https://example.com"),
    rest.WithCORSHeaders("X-Token"),
)
```

| Option | Effect |
|--------|--------|
| `WithCORS(origins ...string)` | Allow the given origins (all origins when empty, no credentials). Entries with a scheme match exactly; a scheme-less host also allows its subdomains |
| `WithCORSHeaders(headers ...string)` | Add allowed request headers to preflight responses; it only configures them, CORS still has to be enabled with `WithCORS` |

`rest.WithCORS*` only mount `resthandler.NewCORSHandler` (`CORSConfig{Origins, Headers}`) at the front of the chain; mount that handler directly with `server.Use` when a `rest.Server` is not used. Preflight requests are answered before the built-in middleware runs. `WithCORSHeaders` prepends a handler built with `CORSConfig{Headers: ...}` and mounted through `AppendHeaders`, which the CORS handler merges into one `Access-Control-Allow-Headers` value, so the option order does not matter. Use `WithNotFoundHandler` when a custom 404 body is needed.
| `WithAuth(secret string, opts ...handler.AuthOption)` | Require a bearer token signed with `secret` on every request. `AuthOption` adds a previous secret, skip paths, a rejection callback and a token extractor |
| `WithRateLimit(limiter ratelimit.Limiter)` | Apply a rate limiter to every request |
| `WithBreaker(br breaker.Breaker)` | Wrap the server with a circuit breaker |
| `WithNotFoundHandler(h http.Handler)` / `WithNotAllowedHandler(h http.Handler)` | Custom 404 / 405 handlers as options (same as `SetNotFoundHandler` / `SetNotAllowedHandler`) |

A real preflight (`OPTIONS` with both `Origin` and `Access-Control-Request-Method`) is answered
with 204 before reaching a route. Plain `OPTIONS` requests still reach the route handler, so custom
OPTIONS semantics keep working. `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` and
`Access-Control-Max-Age` are only emitted on preflight responses.

## Route options

```go
server.AddRoute(rest.Route{
    Method:  http.MethodGet,
    Path:    "/profile",
    Handler: profileHandler,
}, rest.WithRouteAuth("a-long-enough-secret"))
```

| Option | Effect |
|--------|--------|
| `WithRouteMiddleware(mw ...chain.Middleware)` | Wrap only this route handler |
| `WithRouteAuth(secret string, opts ...handler.AuthOption)` | Require a bearer token signed with `secret`, with the same options as `WithAuth` |

## Responses

```go
rest.WriteJSON(r.Context(), w, http.StatusAccepted, body) // explicit status, raw body
rest.OkJSON(w, body)                                      // 200, wrapped in BaseResponse
rest.OkJSONCtx(r.Context(), w, body)                      // 200, wrapped, request_id from ctx
rest.ErrorJSON(w, err)                                    // HTTP status from StatusOf(err)
rest.ErrorJSONCtx(r.Context(), w, err)                    // error envelope with request_id
rest.WriteXML(w, http.StatusAccepted, body)               // explicit status, raw XML
rest.OkXML(w, body)                                       // 200, wrapped in BaseResponse
rest.OkXMLCtx(r.Context(), w, body)                       // 200, wrapped, request_id from ctx
rest.ErrorXML(w, err)
rest.ErrorXMLCtx(r.Context(), w, err)
rest.WriteHTML(w, http.StatusOK, "<h1>ok</h1>")
w.WriteHeader(http.StatusNoContent) // 204, no body
```

`rest.OkJSON`/`rest.OkJSONCtx` and `rest.OkXML`/`rest.OkXMLCtx` wrap the value in `BaseResponse`, so success and
failure share one structure: `{"code":0,"msg":"ok","data":...}` on success and `{"code":-1,"msg":"..."}` on failure.
`request_id` comes from the request context in the `...Ctx` helpers, or from the `X-Request-ID`
response header for the helpers without a context (`OkJSON`, `ErrorJSON`, ...); it stays empty when
the request ID middleware is disabled. XML responses are wrapped in
`<xml version="1.0" encoding="UTF-8">...</xml>`.

Use `rest.WriteJSON`/`rest.WriteXML` when a raw, unwrapped body is needed. `rest.WriteJSON` accepts the request
context for call-site symmetry, but the writers never log, so log returned errors yourself.

`rest.ErrorJSON` and `rest.ErrorXML` hide details for 5xx (the body keeps `code`/`msg` with the HTTP status text) and
echo the message for 4xx. A `StatusError` is an explicit choice, so its message is kept
for 5xx too.

Middleware-generated failures (auth, rate/concurrency limits, timeout, breaker, body
limit, recovery and gunzip) only set the HTTP status and protocol headers
(`Retry-After`, `Connection`) and write no JSON body; this is the handler convention used
throughout the library. Each of them also writes an error log through `core/logger` with the
method, path and resolved client IP. Use `rest.ErrorJSON` or `rest.ErrorXML` in
the business handler when an error envelope is needed.

Status resolution order in `StatusOf`:

1. `StatusCoder` (`StatusCode() int`) implemented by the error chain; values outside 100–999 are
   ignored. The sentinels (`ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrResourceNotFound`,
   `ErrConflict`, `ErrTooManyRequests`, `ErrServerUnavailable`), `CodeMsg` and `StatusError`
   implement it
2. `CodeMsg`: a code in the HTTP 4xx/5xx range is used as-is, any other business code becomes 400
   so the message is preserved
3. Anything else → 500

Decode failures are wrapped in an internal error that implements `StatusCoder`, so `rest.ErrorJSON` maps
them to 400, unsupported content types to 415 and oversized bodies to 413 automatically.

Business errors are created with `rest.NewCodeError(code, msg)`; `ErrorJSON` keeps their `code` and
`msg` in the envelope. Use `rest.NewStatusError(status, msg)` for a custom HTTP status that should
stay an ordinary error (envelope keeps `code: -1` plus the message). Its message is preserved for
5xx as well; only unexpected errors have their detail replaced by `http.StatusText`:

```go
var ErrPaymentRequired = rest.NewStatusError(http.StatusPaymentRequired, "payment required")
```

## Binding

```go
var payload CreateUser
err := rest.Bind(r, &payload)        // content-type driven, query when there is no body
err = rest.BindQuery(r, &payload)    // query string (form tags)
err = rest.BindHeader(r, &payload)   // headers (header tags)
err = rest.BindPath(r, &payload)     // path parameters (path tags)
```

| Source | Tag | Notes |
|--------|-----|-------|
| JSON | `json` | rejects empty, malformed and trailing data |
| XML | `xml` | `application/xml`, `text/xml` |
| Form | `form` | query merged with body, body values win |
| Multipart | `form` | values plus `[]*multipart.FileHeader` fields |
| Query | `form` | single values and repeated keys (`[]string`) |
| Header | `header` | case-insensitive matching |
| Path | `path` | reads `Request.PathValue`; nested fields use their leaf wildcard name |

For a single value, use `rest.PathParam(r, "id")` for a route wildcard such as
`/groups/{id}` or `rest.QueryParam(r, "page")` for the first matching query value.
When the caller needs typed conversions or an emptiness check instead of the raw
string, use `rest.PathParamValue` / `rest.QueryParamValue`, which return a
`cast.Value` (`Int`, `Int64`, `Uint64`, `Float64`, `Bool`, `String`, `IsEmpty`);
the `...Or` variants take the fallback used when the value is empty or not
convertible, for example `rest.QueryParamValue(r, "page").IntOr(1)`.

JSON bodies are also recognised when `Content-Type` is missing or is
`application/x-www-form-urlencoded` (the body starts with `{` or `[` after
whitespace); `application/*+json` media types are accepted. Other explicit media
types keep their normal 415 handling.

Every mapper falls back to the `json` tag when the source tag is absent, and
`"-"` skips a field. Field lookups also try the lower-case form of the key, so an untagged
`Tags []string` field binds `?tags=a&tags=b` and untagged nested structs bind `?profile.name=ada`.

Supported field types: `string`, `bool`, numbers, `[]byte`, `time.Time`
(RFC 3339 or `2006-01-02`, including pointers and slices), `time.Duration`, slices of the above,
`encoding.TextUnmarshaler` and nested/embedded structs (pointers are allocated on demand).

Nested structs are addressed with dotted keys based on the field tag; embedded fields are
flattened:

```go
type Request struct {
    Profile Profile `json:"profile"`
    Owner   *Owner  `json:"owner"`
}

// ?profile.name=ada&owner.email=ada@example.com
```

Path wildcard names must be Go identifiers, so they cannot contain dots. Nested path fields are
therefore declared with their leaf name and mapped back to the dotted key:

```go
type Request struct {
    Org Org `path:"org"`
}
type Org struct {
    ID string `path:"id"` // route: /orgs/{id}
}
```

## Validation

Decoding is followed by validation, in this order:

1. `binding` struct tags, evaluated by `go-playground/validator`
   (`rest/internal/binding.Validator` can be replaced)
2. `Validate() error` on the destination, for rules tags cannot express

```go
type CreateUser struct {
    Name  string `json:"name"  binding:"required,min=2"`
    Email string `json:"email" binding:"required,email"`
}

func (r *CreateUser) Validate() error {
    if strings.HasSuffix(r.Email, "@example.com") {
        return fmt.Errorf("%w: example.com addresses are not allowed", rest.ErrBadRequest)
    }
    return nil
}
```

Binding and validation failures carry a status, so `rest.ErrorJSON(w, err)` answers
400 automatically; an unknown `Content-Type` produces 415.

## handler package

Every handler is a struct with its own `Config` and a `Handle` method. Construct it with
`NewXxxHandler(config)` and pass the method value where a `chain.Middleware` is expected:

```go
type TraceConfig struct {
    Enabled     bool
    IgnorePaths []string
    trace.Config // embedded: name, endpoint, batcher, sampler, ...
}

type TraceHandler struct{ /* ... */ }

func NewTraceHandler(config TraceConfig) *TraceHandler
func (h *TraceHandler) Handle(next http.HandlerFunc) http.HandlerFunc
```

The `Enabled` field makes a handler a no-op when false. `TraceConfig` embeds `trace.Config`, so
the exporter keys (`endpoint`, `batcher`, `sampler`, `name`, ...) sit next to `enabled` in the
config file; `Server.Register` adds the internal trace agent service to the
`core/service.ServiceGroup`, so the group owns starting and stopping it. An agent already
started by the application is reused and left running. The endpoint includes the port, for
example `localhost:4317`. All handlers can be used without a `rest.Server`:

```go
conf.Middlewares.Trace = resthandler.TraceConfig{
    Enabled:     true,
    IgnorePaths: []string{"/health*"},
    Config: trace.Config{
        Name:     "api",
        Batcher:  trace.BatcherOTLPGRPC,
        Endpoint: "localhost:4317", // Jaeger OTLP gRPC; HTTP uses localhost:4318
    },
}
// or, from a config file:
// middlewares:
//   trace:
//     enabled: true
//     endpoint: localhost:4317
//     batcher: otlpgrpc
// Server.Register(group) adds the HTTP server and its auxiliary services.

server.Use(resthandler.NewRateLimitHandler(ratelimit.NewTokenBucket(100, 200)).Handle)
```

| Handler | Behaviour |
|---------|-----------|
| `NewRecoveryHandler(RecoveryConfig)` | Recovers panics → 500, re-panics `http.ErrAbortHandler`; when the response is already committed it aborts the connection instead of hiding the truncation behind a clean 200 |
| `NewRequestIDHandler(RequestIDConfig)` | Keeps a safe `X-Request-ID` or generates a UUID; `resthandler.RequestIDFromContext(ctx)` reads it from the request context. Importing `rest/handler` also registers a `logger` context extractor, so every `logger.XxxCtx` call carries `request_id` automatically |
| `NewTraceHandler(TraceConfig)` | OTel server span; `Name` (service.name attribute), `IgnorePaths` |
| `NewLogHandler(LogConfig, logger)` | Access log via `core/logger`; `SkipPaths`. The service name comes from the logger's `app` field, so it is configured once |
| `NewMaxConnsHandler(MaxConnsConfig)` | Cap in-flight requests → 503 |
| `NewTimeoutHandler(TimeoutConfig)` | Bound handler time → 503 with `Request Timeout`, discards late writes; handler panics are re-raised on the caller goroutine so recovery middleware still handles them. The response is buffered, so streaming is refused: `Flush` returns `http.ErrHandlerTimeout` and `Hijack` returns `http.ErrNotSupported` (leave it off for SSE/WebSocket). |
| `NewMaxBytesHandler(MaxBytesConfig)` | 413 for oversized bodies, `http.MaxBytesReader` for streaming, sets `Connection: close` |
| `NewGunzipHandler(GunzipConfig)` | Transparently decompresses `Content-Encoding: gzip` |
| `NewRateLimitHandler(limiter)` / `NewRateLimitPerSecondHandler(rate, burst)` | 429 via `core/ratelimit`; a nil limiter panics at construction |
| `NewBreakerHandler(br)` | Circuit breaker; 5xx responses count as failures → 503 when open |
| `NewAuthHandler(cfg AuthConfig, opts ...AuthOption)` | Bearer token check built from `cfg.Secret`; any extraction or parse failure answers 401. `AuthConfig` is YAML-loadable (`secret`, `prevSecret`, `skipPaths`), and options override it. `AuthConfig.TokenExtractor` (option `WithTokenExtractor`) replaces `BearerToken` when the token arrives in a query parameter, cookie or custom header |

Default authorization failures write a bodyless 401 and set no challenge header. Set
`WithUnauthorizedCallback` when the application needs a custom body, extra headers or
reporting.

## token package

```go
parser := token.NewParser(token.Config{
    Secret:     "a-long-enough-secret", // required
    PrevSecret: "the-previous-secret",  // optional, accepted while rotating keys
})

signed, err := parser.Generate(jwt.MapClaims{
    "sub":  "user-1",
    "exp":  time.Now().Add(time.Hour).Unix(), // Parse requires exp
    "iss":  "demo",                           // optional
    "role": "admin",                          // any custom claim
})
claims, err := parser.Parse(signed) // jwt.MapClaims as-is
```

`Config` only holds the verification secrets; `Generate` takes the claims map and `Parse` returns
it, so the caller owns every field. `Parse` requires `sub` and `exp` but does not verify `iss`, so
services that must reject other issuers compare `claims["iss"]` themselves.

`handler.AuthHandler` copies the **custom** claims into the request context, one value per
claim name; standard JWT claims (`aud`, `exp`, `iat`, `iss`, `jti`, `nbf`, `sub`) are skipped:

```go
uid, ok := token.Claim(r.Context(), "uid")
if !ok {
    // unauthenticated
}
```

## Service integration

`*rest.Server` has `Start() error` and `Stop() error`, so it plugs straight into
`core/service`. Use `Register` so the configured trace agent service is added to
the group too:

```go
// add application-owned lifecycle services
server.AddService(metricsService)

sg := service.NewServiceGroup()
server.Register(sg) // logger sync + trace agent + AddService services + HTTP server
sg.Start()
```

`server.Register` adds every auxiliary service first and the HTTP server last, so the
ServiceGroup stops the HTTP server before flushing the logger. The logger sync and the trace
agent configured through `Middlewares.Trace` (the embedded `trace.Config`) are added automatically by `NewServer`.

## Notes

- `Server.Stop()` waits `Config.ShutdownTimeout` before forcing connections closed; a zero value
  falls back to 30s instead of closing connections immediately.
- Custom 404/405 handlers keep `http.ServeMux`'s method-aware behaviour: a path match with a
  wrong method stays 405 instead of falling through to 404.
- `internal/response` provides `StatusWriter`, `DeferredWriter` and `BufferedWriter`; they power
  CORS, logging, timeouts and custom fallbacks. `StatusWriter` also forwards `Flush`/`Hijack`, so
  streaming and protocol upgrades survive the logging/tracing/breaker middleware. Informational
  1xx responses (except `101 Switching Protocols`) are forwarded without consuming the final
  status, so the access log and breaker still see the later 2xx-5xx status.
- `WithBreaker` is inserted between the observability middleware and `Timeout`, so a timeout 503 is
  reported to the breaker while the request is still logged and traced.
- An oversized streamed body is answered with 413 and the response carries `Connection: close`, so
  the connection is not reused for another request.
- The access-log `ip` field and middleware error logs resolve the client address from
  `X-Forwarded-For` / `X-Real-IP` (falling back to the direct peer) and strip the port. Use
  `rest.ClientIP` or `rest.ClientIPTrusted` when application code needs a spoof-resistant address.
- A group root route (`Group("/api").Handle("/")`) is served for both `/api` and the exact
  trailing-slash form `/api/`; deeper paths such as `/api/unknown` still return 404.
- `service.AsService` treats `http.ErrServerClosed` as a normal shutdown rather than an error.
