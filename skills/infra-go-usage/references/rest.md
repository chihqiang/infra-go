# rest

HTTP server toolkit: configuration, routing, middleware chain, request binding, response
helpers, JWT based authentication and a set of ready-to-use handlers.

## Module structure

```text
rest/
├── config.go        — Config / MiddlewaresConf / DefaultConfig / Validate
├── server.go        — Server: routing, middleware assembly, start and graceful shutdown
├── route.go         — Route / RouteOption / RouteGroup / Routes listing
├── options.go       — server Option: CORS, TLS, rate limit, breaker, signature
├── response.go      — WriteJSON / OkJSON / CreatedJSON / NoContent / Error, BaseResponse envelope, XML/HTML writers
├── errors.go        — error sentinels, StatusCoder, StatusOf, CodeMsg
├── chain/           — Middleware type and immutable middleware chain
├── router/          — http.ServeMux wrapper, 404/405 detection, path helpers
├── handler/         — struct handlers: Config + NewXxxHandler + Handle
├── token/           — JWT issuing and verification (Parser)
└── internal/        — cors, realip, pathmatch, response writers, binding, service adapters
```

- `chain` has no dependency on the rest of `rest`, so middleware can be reused anywhere.
- `handler` only depends on `chain` + `internal`, so individual handlers can be used without a
  `rest.Server`.
- `internal/*` is not importable from other modules; its behaviour is exposed through `handler`
  and `rest` options.

## Features

- **Routing**: Go 1.22 method-aware patterns (`GET /users/{id}`), nested groups, group middleware
- **Middleware chain**: recovery, request ID, tracing, access log, concurrency limit, timeout,
  body limit, gzip, circuit breaker, rate limit, body signature, JWT auth, CORS
- **Binding**: JSON / XML / form / multipart / query / header / path with self-validation
- **Responses**: `BaseResponse` envelope with `request_id`, JSON/XML/HTML writers, error-to-status mapping
- **Lifecycle**: `Start` / `Serve` / `Shutdown` / `Stop`, with `Register` for `service.ServiceGroup`

## Installation

```bash
go get github.com/chihqiang/infra-go/rest
```

## Quick start

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/rest"
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
        Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := rest.Bind(r, &req); err != nil {
                _ = rest.Error(w, err) // binding failures map to 400/415
                return
            }
            _ = rest.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        }),
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
    ReadTimeout     time.Duration `json:",default=10s"`
    WriteTimeout    time.Duration `json:",default=10s"`
    IdleTimeout     time.Duration `json:",default=120s"`
    ShutdownTimeout time.Duration `json:",default=10s"`
    MaxHeaderBytes  int           `json:",default=1048576"`
    Logger          logger.Config
    Middlewares     MiddlewaresConf
}

type MiddlewaresConf struct {
    Recovery  resthandler.RecoveryConfig
    RequestID resthandler.RequestIDConfig
    Trace     resthandler.TraceConfig
    Log       resthandler.LogConfig
    MaxConns  resthandler.MaxConnsConfig
    Auth      resthandler.AuthConfig
    RateLimit resthandler.RateLimitConfig
    Breaker   resthandler.BreakerConfig
    Timeout   resthandler.TimeoutConfig
    Gunzip    resthandler.GunzipConfig
    MaxBytes  resthandler.MaxBytesConfig
    Signature resthandler.SignatureConfig
}
```

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

```go
server, _ := rest.NewServer(rest.Config{
    Logger: logger.Config{
        ServiceName: "api",
        Mode:        "file",
        Path:        "/var/log/api",
        Level:       "info",
        Rotation:    "size",
        MaxSize:     100,
        MaxBackups:  7,
        KeepDays:    30,
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
        Enabled:        true,
        ServiceName:    "api",
        SkipPaths:      []string{"/healthz"},
        TrustedProxies: []string{"10.0.0.0/8"},
    },
    Trace: resthandler.TraceConfig{
        Enabled:     true,
        ServiceName: "api",
        IgnorePaths: []string{"/health*"},
    },
    MaxBytes: resthandler.MaxBytesConfig{Enabled: true, Limit: 1 << 20},
    Signature: resthandler.SignatureConfig{
        Enabled: true,
        Keys:    [][]byte{signingKey},
    },
},
```

`Auth`, `RateLimit` and `Breaker` also have `Enabled` switches, but their parser/limiter/breaker
are runtime dependencies (`json:"-"`): set them programmatically after loading config.
`Config.Validate` rejects enabling one of them without its dependency.

Tags are consumed by `core/conf` and `core/mapping`, so the same struct works for YAML/JSON files
and environment expansion. `Config.Validate()` rejects an out-of-range port, half-configured TLS,
invalid trusted proxies and negative limits.

`Middlewares.Log.TrustedProxies` accepts IPs and CIDR ranges. When it is set, the access log only
honours `X-Forwarded-For` / `X-Real-IP` if the direct peer is in the list, and walks the forwarded
chain right-to-left; when it is empty the forwarding headers are ignored and the direct peer
address is used. `ClientIP` returns the direct peer as well; `ClientIPFromHeaders` keeps the legacy
trust-any-peer behaviour, and `ClientIPTrusted` / `ParseTrustedProxies` expose the proxy-aware
logic for application code.

Middleware execution order is fixed by the server:

```text
Recover → RequestID → Trace → Log → MaxConns → RateLimit → Auth → [Breaker] → Timeout → Gunzip → MaxBytes → Signature → Use(...) → route
```

`Gunzip` runs before `MaxBytes` so the body limit applies to the decompressed payload; `Signature` runs after `MaxBytes` so it sees the bounded, decompressed body.

### Request signing

The signed payload binds a signature to one endpoint:

```text
METHOD \n escaped-path \n raw-query \n timestamp \n body
```

Clients sign with the same request object they send:

```go
timestamp := time.Now().Unix()
req.Header.Set(resthandler.HeaderTimestamp, strconv.FormatInt(timestamp, 10))
req.Header.Set(resthandler.HeaderSignature, resthandler.SignRequest(key, timestamp, req, body))
```

`TTL` defaults to 5 minutes when omitted; a negative value disables freshness checking
explicitly (replay protection is lost). Servers verify with `X-Signature` / `X-Signature-Timestamp`.
CORS wraps the whole chain, so 404/405 responses carry CORS headers too.

## Server

| Method | Purpose |
|--------|---------|
| `NewServer(conf Config, opts ...Option) (*Server, error)` | Build a server, validating config |
| `Handle(method, path string, h http.Handler)` / `HandleFunc` | Register a single route |
| `AddRoute(Route, ...RouteOption)` / `AddRoutes(...Route)` | Register routes with options |
| `Group(prefix string) *RouteGroup` | Route group, embeddable/nestable, with `Use` |
| `Use(chain.Middleware)` | Append a server-wide middleware |
| `SetNotFoundHandler` / `SetNotAllowedHandler` | Custom 404 / 405 handlers |
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
| `WithCORSHeaders(headers ...string)` | Append allowed request headers; it only configures them, CORS still has to be enabled with `WithCORS` / `WithCustomCORS` |
| `WithCustomCORS(headerFn, notAllowedFn, origins...)` | Customise headers and the rejected-request body |

A real preflight (`OPTIONS` with both `Origin` and `Access-Control-Request-Method`) is answered
with 204 before reaching a route. Plain `OPTIONS` requests still reach the route handler, so custom
OPTIONS semantics keep working. `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` and
`Access-Control-Max-Age` are only emitted on preflight responses.

## Route options

```go
parser, _ := token.NewParser(token.Config{Secret: "a-long-enough-secret"})

server.AddRoute(rest.Route{
    Method:  http.MethodGet,
    Path:    "/profile",
    Handler: profileHandler,
}, rest.WithRouteAuth(parser))
```

| Option | Effect |
|--------|--------|
| `WithRouteMiddleware(mw ...chain.Middleware)` | Wrap only this route handler |
| `WithRouteAuth(parser *token.Parser)` | Require a valid bearer token; custom config via `WithRouteMiddleware(handler.NewAuthHandler(...).Handle)` |

## Responses

```go
rest.WriteJSON(w, http.StatusAccepted, body) // explicit status
rest.OkJSON(w, body)                         // 200
rest.CreatedJSON(w, body)                    // 201
rest.NoContent(w)                            // 204, no body
rest.Error(w, err)                           // HTTP status from StatusOf(err)
```

`rest.Error` hides details for 5xx (body keeps `code`/`msg`, the real error is logged) and echoes
the message for 4xx.

Middleware-generated failures (auth, rate/concurrency limits, timeout, breaker, signature, body
limit, recovery and gunzip) only set the HTTP status and protocol headers
(`Retry-After`, `WWW-Authenticate`, `Connection`) and write no JSON body, matching go-zero's
handler convention. Use `rest.Error` in the business handler when an error envelope is needed.

Status resolution order in `StatusOf`:

1. `StatusCoder` (`StatusCode() int`) implemented by the error chain; values outside 100–999 are
   ignored
2. `CodeMsg`: a code in the HTTP 4xx/5xx range is used as-is, any other business code becomes 400
   so the message is preserved
3. Sentinels: `ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrResourceNotFound`, `ErrConflict`,
   `ErrTooManyRequests`, `ErrServerUnavailable`
4. Anything else → 500

Decode failures are wrapped in an internal error that implements `StatusCoder`, so `rest.Error` maps
them to 400, unsupported content types to 415 and oversized bodies to 413 automatically.

Business errors are created with `rest.NewCodeError(code, msg)`.

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

JSON bodies are also recognised when `Content-Type` is missing or is
`application/x-www-form-urlencoded` (the body starts with `{` or `[` after
whitespace); `application/*+json` media types are accepted. Other explicit media
types keep their normal 415 handling.

Every mapper falls back to the `json` tag when the source tag is absent, and
`"-"` skips a field.

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

Binding and validation failures carry a status, so `rest.Error(w, err)` answers
400 automatically; an unknown `Content-Type` produces 415.

## handler package

Every handler is a struct with its own `Config` and a `Handle` method. Construct it with
`NewXxxHandler(config)` and pass the method value where a `chain.Middleware` is expected:

```go
type TraceConfig struct {
    Enabled     bool
    ServiceName string
    IgnorePaths []string
    Agent       trace.Config
}

type TraceHandler struct{ /* ... */ }

func NewTraceHandler(config TraceConfig) *TraceHandler
func (h *TraceHandler) Handle(next http.HandlerFunc) http.HandlerFunc
```

The `Enabled` field makes a handler a no-op when false. `TraceConfig.Agent` configures the
tracing exporter; `Server.Register` adds the internal trace agent service to the
`core/service.ServiceGroup`, so the group owns starting and stopping it. An agent already
started by the application is reused and left running. The endpoint includes the port, for
example `localhost:4317`. All handlers can be used without a `rest.Server`:

```go
conf.Middlewares.Trace = resthandler.TraceConfig{
    Enabled:     true,
    ServiceName: "api",
    IgnorePaths: []string{"/health*"},
    Agent: trace.Config{
        Batcher:  trace.BatcherOTLPGRPC,
        Endpoint: "localhost:4317", // Jaeger OTLP gRPC; HTTP uses localhost:4318
    },
}
// Server.Register(group) adds the HTTP server and its auxiliary services.

server.Use(resthandler.NewRateLimitHandler(resthandler.RateLimitConfig{
    Enabled: true,
    Limiter: ratelimit.NewTokenBucket(100, 200),
}).Handle)
```

| Handler | Behaviour |
|---------|-----------|
| `NewRecoveryHandler(RecoveryConfig)` | Recovers panics → 500, re-panics `http.ErrAbortHandler` |
| `NewRequestIDHandler(RequestIDConfig)` | Keeps a safe `X-Request-ID` or generates a UUID; `ContextWithRequestID` / `RequestIDFromContext` |
| `NewTraceHandler(TraceConfig)` | OTel server span; `ServiceName`, `IgnorePaths` |
| `NewLogHandler(LogConfig, logger)` | Access log via `core/logger`; `ServiceName`, `SkipPaths`, `TrustedProxies`, `ClientIP` |
| `NewMaxConnsHandler(MaxConnsConfig)` | Cap in-flight requests → 503 |
| `NewTimeoutHandler(TimeoutConfig)` | Bound handler time → 503 with `Request Timeout`, discards late writes; handler panics are re-raised on the caller goroutine so recovery middleware still handles them. The response is buffered, so streaming is refused: `Flush` returns `http.ErrHandlerTimeout` and `Hijack` returns `http.ErrNotSupported` (leave it off for SSE/WebSocket). |
| `NewMaxBytesHandler(MaxBytesConfig)` | 413 for oversized bodies, `http.MaxBytesReader` for streaming, sets `Connection: close` |
| `NewGunzipHandler(GunzipConfig)` | Transparently decompresses `Content-Encoding: gzip` |
| `NewRateLimitHandler(RateLimitConfig)` / `NewRateLimitPerSecondHandler(rate, burst)` | 429 via `core/ratelimit` |
| `NewBreakerHandler(BreakerConfig)` | Circuit breaker; 5xx responses count as failures → 503 when open |
| `NewSignatureHandler(SignatureConfig)` | HMAC check over `METHOD / escaped-path / raw-query / timestamp / body` with key rotation and a timestamp TTL (zero uses the 5-minute default, negative disables it); the timestamp is checked before the body is read |
| `NewAuthHandler(AuthConfig)` | Bearer token check; RFC 6750 challenge (`invalid_request` → 400, `invalid_token` → 401), missing parser → 500, optional `Callback` |

Default authorization failures write 400/401/500 with an RFC 6750 `WWW-Authenticate` challenge
and no body. Set `AuthConfig.Callback` when the application needs a custom body or extra
reporting.

## token package

```go
parser, err := token.NewParser(token.Config{
    Secret:     "a-long-enough-secret", // >= 8 chars
    PrevSecret: "the-previous-secret",  // optional, accepted while rotating keys
    Expire:     24 * time.Hour,
    Issuer:     "demo",
})

signed, err := parser.Generate("user-1", map[string]any{"role": "admin"})
claims, err := parser.Parse(signed)      // Subject / Issuer / ExpiresAt / Data
```

`Parse` verifies `iss` when `Issuer` is configured and requires the `exp` claim. `PrevSecret`
must satisfy the same 8-character minimum as `Secret`.

`handler.AuthHandler` stores the verified claims in the request context:

```go
claims, ok := token.ClaimsFrom(r.Context())
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
agent configured through `Middlewares.Trace.Agent` are added automatically by `NewServer`.

## Notes

- `Server.Stop()` waits `Config.ShutdownTimeout` before forcing connections closed; a zero value
  falls back to 10s instead of closing connections immediately.
- Custom 404/405 handlers keep `http.ServeMux`'s method-aware behaviour: a path match with a
  wrong method stays 405 instead of falling through to 404.
- `internal/response` provides `StatusWriter`, `DeferredWriter` and `BufferedWriter`; they power
  CORS, logging, timeouts and custom fallbacks. `StatusWriter` also forwards `Flush`/`Hijack`, so
  streaming and protocol upgrades survive the logging/tracing/breaker middleware.
- `WithBreaker` is inserted between the observability middleware and `Timeout`, so a timeout 503 is
  reported to the breaker while the request is still logged and traced.
- An oversized streamed body is answered with 413 and the response carries `Connection: close`, so
  the connection is not reused for another request.
- `ClientIP` and the access-log `ip` field use the direct peer address and ignore forwarding
  headers by default. Set `Middlewares.Log.TrustedProxies` (access log) or use `ClientIPTrusted`
  (application code) to trust them from known proxies; `ClientIPFromHeaders` is the legacy
  trust-any-peer helper.
- A group root route (`Group("/api").Handle("/")`) is served for both `/api` and the exact
  trailing-slash form `/api/`; deeper paths such as `/api/unknown` still return 404.
- `service.AsService` treats `http.ErrServerClosed` as a normal shutdown rather than an error.
