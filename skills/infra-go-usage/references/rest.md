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
├── handler/         — middleware implementations (all return chain.Middleware)
├── token/           — JWT issuing and verification (Parser)
└── internal/        — cors, realip, pathmatch, response writers, binding
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
- **Lifecycle**: `Start` / `Serve` / `Shutdown` / `Stop`, compatible with `service.AsService`

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

`rest.DefaultConfig()` fills host/port, timeouts, `MaxBodyBytes` and the default middleware
switches. A zero `Config` is also valid and starts with all middleware disabled.

The package name `rest` does not collide with `net/http`, so handlers can import both directly.

## Configuration

```go
type Config struct {
    Name             string        `json:",optional"` // service name for spans and access logs
    Host            string        `json:",default=0.0.0.0"`
    Port            int           `json:",default=8080,range=[0:65535]"`
    CertFile        string        `json:",optional"`
    KeyFile         string        `json:",optional"`
    ReadTimeout     time.Duration `json:",default=10s"`
    WriteTimeout    time.Duration `json:",default=10s"`
    IdleTimeout     time.Duration `json:",default=120s"`
    ShutdownTimeout time.Duration `json:",default=10s"`
    MaxHeaderBytes  int           `json:",default=1048576"`
    MaxBodyBytes    int64         `json:",default=1048576"`
    Middlewares      MiddlewaresConf
    TrustedProxies   []string `json:",optional"`
    TraceIgnorePaths []string `json:",optional"`
    LogSkipPaths     []string `json:",optional"`
}

type MiddlewaresConf struct {
    Recovery  bool          `json:",default=true"`
    RequestID bool          `json:",default=true"`
    Trace     bool          `json:",default=false"`
    Log       bool          `json:",default=true"`
    MaxConns  int           `json:",default=0"`
    Timeout   time.Duration `json:",default=0"`
    Gunzip    bool          `json:",default=false"`
}
```

Tags are consumed by `core/conf` and `core/mapping`, so the same struct works for YAML/JSON files
and environment expansion. `Config.Validate()` rejects an out-of-range port, half-configured TLS,
invalid trusted proxies and negative limits.

`TrustedProxies` accepts IPs and CIDR ranges. When it is set, the access log only honours
`X-Forwarded-For` / `X-Real-IP` if the direct peer is in the list, and walks the forwarded chain
right-to-left; when it is empty the forwarding headers are ignored and the direct peer address is
used. `ClientIP` returns the direct peer as well; `ClientIPFromHeaders` keeps the legacy
trust-any-peer behaviour, and `ClientIPTrusted` / `ParseTrustedProxies` expose the proxy-aware
logic for application code.

Middleware execution order is fixed by the server:

```text
Recover → RequestID → Trace → Log → MaxConns → [Breaker] → Timeout → Gunzip → MaxBodyBytes → Use(...) → route
```

`Gunzip` runs before `MaxBodyBytes` so the body limit applies to the decompressed payload.
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
| `Addr()`, `Router()`, `ServeHTTP` | Introspection and `httptest` usage |

```go
server, _ := rest.NewServer(rest.Config{
    Host: "127.0.0.1",
    Port: 0,
    Middlewares: rest.MiddlewaresConf{Recovery: true, RequestID: true},
})

api := server.Group("/api")          // "/api"
v1 := api.Group("/v1")               // "/api/v1"
v1.Use(resthandler.TimeoutHandler(time.Second))
v1.HandleFunc(http.MethodGet, "/users/{id}", handler)
```

Group middleware applies to routes registered after `Use` and is inherited by child groups.

## Server options

```go
server, _ := rest.NewServer(conf,
    rest.WithCors("https://example.com"),
    rest.WithCorsHeaders("X-Token"),
)
```

| Option | Effect |
|--------|--------|
| `WithCors(origins ...string)` | Allow the given origins (all origins when empty, no credentials). Entries with a scheme match exactly; a scheme-less host also allows its subdomains |
| `WithCorsHeaders(headers ...string)` | Append allowed request headers; it only configures them, CORS still has to be enabled with `WithCors` / `WithCustomCors` |
| `WithCustomCors(headerFn, notAllowedFn, origins...)` | Customise headers and the rejected-request body |

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
| `WithRouteAuth(parser *token.Parser, opts ...handler.AuthorizeOption)` | Require a valid bearer token |

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
3. Sentinels: `ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrConflict`,
   `ErrTooManyRequests`, `ErrServiceUnavailable`
4. Anything else → 500

Decode failures are wrapped in an internal error that implements `StatusCoder`, so `rest.Error` maps
them to 400, unsupported content types to 415 and oversized bodies to 413 automatically.

Business errors are created with `rest.NewCodeError(code, msg)`; `rest.New` remains as a deprecated
alias.

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

Every handler is a `chain.Middleware`, i.e. `func(http.HandlerFunc) http.HandlerFunc`, and can be
used without a `rest.Server`:

| Handler | Behaviour |
|---------|-----------|
| `RecoverHandler` | Recovers panics → 500, re-panics `http.ErrAbortHandler` |
| `RequestIDHandler` | Keeps a safe `X-Request-ID` or generates a UUID; `RequestIDFromContext` |
| `TraceHandler(opts...)` | OTel server span; `WithTraceServiceName`, `WithTraceIgnorePaths` |
| `LogHandler(opts...)` / `LogHandlerWith(log, opts...)` | Access log via `core/logger` with method/pattern/status/duration/ip/request_id; `WithLogSkipPaths`, `WithLogServiceName` |
| `MaxConnsHandler(n)` | Cap in-flight requests → 503 |
| `TimeoutHandler(d)` | Bound handler time → 503 with `Request Timeout` (go-zero style), discards late writes; handler panics are re-raised on the caller goroutine so recovery middleware still handles them |
| `MaxBytesHandler(n)` | 413 for oversized bodies, `http.MaxBytesReader` for streaming, sets `Connection: close` |
| `GunzipHandler` | Transparently decompresses `Content-Encoding: gzip` |
| `RateLimitHandler(limiter)` / `RateLimitPerSecondHandler(rate, burst)` | 429 via `core/ratelimit` |
| `BreakerHandler(br)` | Circuit breaker; 5xx responses count as failures → 503 when open |
| `SignatureHandler(cfg)` | HMAC-signed body check with key rotation and timestamp TTL; the timestamp is checked before the body is read |
| `Authorize(parser, opts...)` | Bearer token check; RFC 6750 challenge (`invalid_request` → 400, `invalid_token` → 401), missing parser → 500, `WithUnauthorizedCallback` |

Default authorization failures write 400/401/500 with an RFC 6750 `WWW-Authenticate` challenge
and no body. Install `WithUnauthorizedCallback` when the application needs a custom body or extra
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

`handler.Authorize` stores the verified claims in the request context:

```go
claims, ok := token.ClaimsFrom(r.Context())
```

## Service integration

`*rest.Server` has `Start() error` and `Stop() error`, so it plugs straight into
`core/service`:

```go
sg := service.NewServiceGroup()
sg.Add(service.AsService(server))
sg.Start()
```

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
  headers by default. Set `Config.TrustedProxies` (access log) or use `ClientIPTrusted`
  (application code) to trust them from known proxies; `ClientIPFromHeaders` is the legacy
  trust-any-peer helper.
- A group root route (`Group("/api").Handle("/")`) is served for both `/api` and the exact
  trailing-slash form `/api/`; deeper paths such as `/api/unknown` still return 404.
- `service.AsService` treats `http.ErrServerClosed` as a normal shutdown rather than an error.
