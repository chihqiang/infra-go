# httpx

HTTP service infrastructure, located in the `httpx` directory. The main package provides the server, unified responses, request binding helpers and middleware adapters; core capabilities are split into several subpackages by responsibility, all of which can be reused by other `net/http`-compatible frameworks.

## Module structure

```text
httpx/
├── request.go              — request binding helpers (Bind*/MustBind*) + single-value reads (QueryValue/PathValue/HeaderValue)
├── response.go             — unified responses: Response[T]/CodeError/Ok*/Write*/SSEWriter/Redirect*
├── server.go               — server core: Server/Route/ServerConfig types, route registration/middleware chain, Handler/404, start and graceful shutdown
├── server_options.go       — options and adapters: RouteOption/RunOption, With*/Apply*, AsMiddleware
├── server_group.go         — route groups: Group (prefix + middleware, nestable)
├── internal_middleware.go  — built-in middleware adapter layer: With* (forwards to httpx/middleware)
├── internal_route.go       — built-in routes (PprofRoutes)
├── ctx.go                  — request_id context helpers (delegates to httpx/middleware)
├── binding/                — request binding implementation (binder/mapping engine/validator, see below)
├── middleware/             — general middleware implementations (object-oriented, one middleware per file)
├── respw/                  — ResponseWriter enhancements (see [httpx-respw](./httpx-respw.md))
└── x/                      — general HTTP utilities (path matching + client IP resolution, see [httpx-x](./httpx-x.md))
```

- `httpx/binding`: binder interfaces/instances, MIME constants, the reflection mapping engine and the validator (`SetValidateFn`).
- `httpx/middleware`: the core logic of each middleware, in the object-oriented `NewXxx(...)` + `Middleware()` form, returning the standard `func(http.Handler) http.Handler`, independent of httpx and directly reusable by gin / echo and others (see the "Middleware" section).
- The `httpx` main package = server + unified responses + binding helpers + the `With*` adapter layer, and is the main entry point for business projects.

## Features

- **Unified responses**: the `Response[T]` struct plus smart wrapping in `Ok*` / `WriteHTTPError*`
- **Parameter binding**: six sources — JSON / XML / Form / Query / Header / URI — plus `binding` tag validation
- **General middleware**: CORS, Recovery, RequestID, tracing, access logs, breaker, timeout, request body limits, gzip decompression, concurrency limits, rate limiting, JWT auth, encryption/decryption, content security
- **Reusable**: the middleware core logic lives in the `httpx/middleware` subpackage in standard `net/http` form and can be reused by any framework
- **Server routing**: Go 1.22 `{param}` path parameters, route groups, middleware chains, graceful shutdown, pprof

## Installation

```bash
go get github.com/chihqiang/infra-go/httpx
```

## Quick start

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/httpx"
    "github.com/chihqiang/infra-go/httpx/middleware" // optional: use subpackage middleware directly
)

func main() {
    server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})

    // middleware (With* registers conveniently; equivalent to middleware.NewXxx().Middleware() via AsMiddleware)
    server.Use(httpx.WithRecovery())
    server.Use(httpx.WithRequestID())
    server.Use(httpx.WithLogger("/healthz"))
    server.Use(httpx.WithCors("*"))

    server.AddRoute(httpx.Route{
        Method: "POST",
        Path:   "/users",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req CreateUserRequest
            if err := httpx.MustBindJSON(w, r, &req); err != nil {
                return // 400 already written automatically
            }
            httpx.OkJSON(w, map[string]any{"id": "user-1"}) // wrapped into Response[T] automatically
        },
    })

    server.Start() // blocking; graceful shutdown on SIGINT/SIGTERM/SIGHUP
}
```

## Request binding

Binds request data into structs by source. Fields declare their source name through a tag, and validation rules can be declared with the `binding` tag.

> The binder **implementation** lives in the `httpx/binding` subpackage (binder instances `binding.JSON/XML/Form/Query/Header/Uri`, the `binding.Default` selector, the reflection mapping engine and the `SetValidateFn` validation entry). The httpx main package's `Bind*` / `MustBind*` helpers call that subpackage internally, so business code normally just uses the main package functions.

### Supported tags

| Tag | Applicable source | Description |
|------|---------|------|
| `json` | JSON body | JSON field name |
| `xml` | XML body | XML field name |
| `form` | Form / Query | Form / Query parameter name |
| `uri` | URI | Path parameter name |
| `header` | Header | HTTP header name (case-insensitive) |
| `binding` | All | Validation rules, see [Parameter validation](#parameter-validation) |
| `default` | Form / Query / Header / URI | Field default value, e.g. `form:"sort,default=desc"` |
| `time_format` / `time_utc` / `time_location` | Form / Query | Time parsing control |
| `-` | All | Ignore the field; don't bind it |

### Binding functions

| Function | Data source | Description |
|------|---------|------|
| `BindJSON(r, &obj)` | JSON body | |
| `BindXML(r, &obj)` | XML body | |
| `BindForm(r, &obj)` | Query + POST form | |
| `BindQuery(r, &obj)` | URL Query | |
| `BindHeader(r, &obj)` | HTTP Header | |
| `BindURI(params, &obj)` | Path parameters | `params` is a `map[string]string` |
| `BindURIWithValues(params, &obj)` | Path parameters | `params` is a `map[string][]string` |
| `Bind(r, &obj)` | Automatic | Chooses based on Method / Content-Type |
| `MustBind*` | — | Binding + automatic error response |

### Examples

```go
// JSON
type LoginRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required,min=6"`
}
var req LoginRequest
if err := httpx.BindJSON(r, &req); err != nil {
    httpx.WriteHTTPError(w, http.StatusBadRequest, err.Error())
    return
}

// Query (form tag, supports default)
type ListRequest struct {
    Page     int    `form:"page" binding:"gte=1"`
    PageSize int    `form:"page_size" binding:"gte=1,lte=100"`
    Sort     string `form:"sort,default=desc"`
}
if err := httpx.BindQuery(r, &req); err != nil { /* handle */ }

// Header
type AuthRequest struct {
    Token   string `header:"X-Auth-Token" binding:"required"`
    Version string `header:"X-Version,default=v1"`
}
if err := httpx.BindHeader(r, &req); err != nil { /* handle */ }

// URI (path parameter /users/{id})
type GetUserRequest struct {
    ID int `uri:"id" binding:"required"`
}
params := map[string]string{"id": r.PathValue("id")}
if err := httpx.BindURI(params, &req); err != nil { /* handle */ }
```

### Automatic selection rules

`Bind(r, &obj)` chooses based on Method and Content-Type: GET → Form (Query); POST + `application/json` → JSON; `application/xml`/`text/xml` → XML; `application/x-www-form-urlencoded` / `multipart/form-data` → Form; anything else / unparsable → Form.

### MustBind — binding plus automatic error response

When binding or validation fails it writes the HTTP error response automatically (carrying `request_id`) and returns an error so the control flow can react:

```go
if err := httpx.MustBindJSON(w, r, &req); err != nil {
    return // 400 already written automatically
}
// same family: MustBind (automatic selection) / MustBindQuery / MustBindForm
```

### Single-value reads — QueryValue / PathValue / HeaderValue

Reads a single value by key and converts its type (reusing `cast.ToE` under the hood), with no struct definition needed:

```go
page  := httpx.QueryValue(r, "page", 1)     // int; missing/invalid → 1
tag   := httpx.QueryValue[string](r, "tag") // string; missing → ""
id    := httpx.PathValue(r, "id", int64(0)) // path parameter {id}
token := httpx.HeaderValue(r, "X-Token", "")
```

## Parameter validation

After the binder has mapped the data, the `binding` tag rules are validated automatically (based on [go-playground/validator/v10](https://github.com/go-playground/validator)):

```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=20"`
    Password string `json:"password" binding:"required,min=8"`
    Email    string `json:"email" binding:"required,email"`
    Role     string `json:"role" binding:"required,oneof=admin user guest"`
}
```

Common rules: `required` for mandatory; `min=N`/`max=N`; `gte=N`/`lte=N`; `email`/`url`; `oneof=a b c` for enums; `len=N`.

The default validator is `DefaultValidator` from the `httpx/binding` subpackage (tag `binding`). To use a custom validator, implement `binding.StructValidator` and inject the validation entry point through `binding.SetValidateFn` (see the appendix); passing `nil` restores the default. The httpx main package binding helpers and the binding binders share the same validation entry point.

## Supported data types

| Type | Example |
|------|------|
| `string` | `Name string \`form:"name"\`` |
| `int/int8/int16/int32/int64`, `uint/.../uint64` | `Age int \`form:"age"\`` |
| `bool` | `Active bool \`form:"active"\`` |
| `float32/float64` | `Score float64 \`form:"score"\`` |
| `time.Time` | supports `time_format`/`time_utc`/`time_location` and Unix timestamps |
| `time.Duration` | `Timeout time.Duration \`form:"timeout"\`` (e.g. `1m30s`) |
| Slices such as `[]string` | collected automatically from comma-separated values or repeated keys |
| Embedded structs / `map[string]string` targets | recursive / direct filling |

## Unified responses

### The Response[T] struct

```go
type Response[T any] struct {
    Code      int    `json:"code" xml:"code"`
    Msg       string `json:"msg" xml:"msg"`
    Data      T      `json:"data,omitempty" xml:"data,omitempty"`
    RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
}
```

### Smart wrapping (recommended)

The `Ok*` family wraps `v` into `Response[T]` automatically; if `v` is a `*CodeError` / `CodeError` / `error`, the matching business code and message are taken from it:

```go
httpx.OkJSON(w, data)          // {code:0,msg:"ok",data:...}
httpx.OkJSONCtx(ctx, w, data)  // the response carries request_id
httpx.OkXML(w, data)
httpx.OkXMLCtx(ctx, w, data)
httpx.OkHTML(w, "<h1>hi</h1>")
httpx.OkHTMLCtx(ctx, w, "<h1>hi</h1>")
```

### Low-level output

```go
httpx.WriteJSON(w, status, v)        // any status code + any structure
httpx.WriteJSONCtx(ctx, w, status, v)
httpx.WriteXML(w, status, v)
httpx.WriteXMLCtx(ctx, w, status, v)
```

### Error responses

```go
httpx.WriteHTTPError(w, status, msg)                       // code = status
httpx.WriteHTTPErrorCtx(ctx, w, status, msg)               // with request_id
httpx.WriteHTTPErrorWithCode(w, status, code, msg)         // business code separated from the HTTP code
httpx.WriteHTTPErrorWithCodeCtx(ctx, w, status, code, msg)
```

Business code constants: `CodeOK = 0`, `MsgOK = "ok"`, `CodeDefaultError = -1`.

### CodeError

```go
httpx.NewCodeError(code, msg)                 // business error
httpx.NewCodeErrorWithCause(code, msg, err)   // with a root cause, usable with errors.Is/As
```

### SSE streaming responses

```go
sse := httpx.NewSSEWriter(w)                    // the constructor takes only w
sse.Event("message", `{"a":1}`)                // write an event+data frame
sse.JSONEvent("message", map[string]any{"a": 1}) // v is serialized to JSON and written as data
sse.Data("ping")                                // write data for the default message event
sse.Comment("keepalive")
sse.Retry(3000)        // reconnection interval after a drop
sse.Flush()
```

> `WithTimeout` exempts long-lived SSE/WebSocket connections.

### Redirects

```go
httpx.Redirect(w, r, url, http.StatusFound)   // RedirectCtx / RedirectTemporary / RedirectTemporaryCtx
```

## Request ID

`WithRequestID` reads the `X-Request-Id` header (generating a uuid by default), injects it into the context and writes it back to the response headers. Reading it downstream:

```go
id := httpx.RequestIDFromContext(r.Context())
```

> The request_id context key and its accessors live in `httpx/middleware` (`middleware.ContextWithRequestID`/`RequestIDFromContext`) and httpx's `ctx.go` delegates to them; combined with `OkJSONCtx`/`WriteHTTPErrorCtx` it appears automatically in the response's `request_id` field, and combined with `logger.XxxCtx` it appears automatically in logs.

## Routing and the server

### Creating a server

```go
server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})
```

### Route and registration

```go
server.AddRoute(httpx.Route{Method: "GET", Path: "/users", Handler: handler})
server.AddRoutes([]httpx.Route{{...}, ...}, httpx.WithPrefix("/api/v1"))
server.Routes() // returns copies of all routes
```

### Path parameters

Go 1.22 route patterns `{param}`:

```go
server.AddRoute(httpx.Route{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    httpx.OkJSON(w, id)
}})
```

### Middleware

```go
type Middleware func(http.HandlerFunc) http.HandlerFunc

server.Use(mw1, mw2)              // global middleware (applies to every route)
server.AddRoutes(rs, httpx.WithMiddleware(authMW)) // middleware for a single route group
server.AddRoutes(httpx.ApplyMiddleware(authMW, routes...)) // wrap routes directly
```

Execution order: global middleware (`Use`) → group middleware → route handler.

### Route groups (Group)

```go
api := server.Group("/api", authMW)
v1 := api.Group("/v1", logMW) // prefix /api/v1, middleware stacks up
v1.AddRoute(httpx.Route{...})
```

### Custom 404

```go
server.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
    httpx.OkJSON(w, "not found")
})
```

Notes:

- It applies only to requests where **no route matched**; a 404 returned deliberately inside a business route (such as "resource not found") is not hijacked and is passed to the client as-is.
- A path that matches but doesn't allow the method (405) is not taken over; `405 Method Not Allowed` and the `Allow` header are still returned.
- The handler must write the status code itself; when `WriteHeader` isn't called net/http writes 200 implicitly (the `OkJSON` family relies on this convention: HTTP 200 plus a business error code in the body).
- After setting this handler, normal route handlers still get the full `http.Flusher` / `http.Hijacker` / `http.Pusher` / `Unwrap` capabilities (SSE, WebSocket and HTTP/2 Push are unaffected).

### panic recovery

`WithRecovery` catches handler panics, logs the stack trace and returns 500.

### Start and shutdown

```go
server.Start()    // blocking; graceful shutdown on SIGINT/SIGTERM/SIGHUP
server.Shutdown() // manual shutdown (returns an error)
server.Stop()     // stop; delegates to Shutdown (returns an error), so service.AsService can manage it in a ServiceGroup
```

## Middleware

httpx middleware comes in two layers:

1. **The `httpx/middleware` subpackage (implementation layer)**: one file and one type per middleware; `NewXxx(...)` constructs it (doing parameter precomputation), and `(m *Xxx) Middleware()` returns the standard `func(http.Handler) http.Handler`. It does not depend on httpx, so gin/echo/the standard library can reuse it. Error responses are resolved per **request scope**: the httpx Server injects the unified JSON renderer into each request's context, while the subpackage itself keeps the default `http.Error`.
2. **The `httpx` main package (adapter layer)**: the `WithXxx(...)` helpers adapt the subpackage's standard middleware into `httpx.Middleware` for registration with `server.Use`, with stable method signatures.

### Built-in middleware (httpx.With*)

| Middleware | Purpose |
|--------|------|
| `WithCors(origins...)` | CORS; `"*"` allows every origin (**echoes the concrete Origin**, never sending a wildcard); no headers for same-origin; **unauthorized origins pass through by default** (no CORS headers emitted); OPTIONS preflight → 204 |
| `WithRecovery()` | panic recovery → 500 + stack trace log |
| `WithRequestID()` | inject request_id into the context / write it back to the response headers |
| `WithTracing(ignorePaths...)` | distributed tracing (server-side span, using the global TracerProvider by default) |
| `WithLogger(skipPaths...)` | access logs (method/path/status/bytes/latency) |
| `WithBreaker()` | global breaker (a single global breaker; 503 + `Retry-After` while open) |
| `WithRouteBreaker()` | per-route breaker (keyed by the **route pattern**, see below; 503 + `Retry-After` while open) |
| `WithTimeout(d)` | request timeout (WS/SSE exempt; 499 when the client disconnects) |
| `WithMaxBytes(n)` | request body size limit (413) |
| `WithGunzip()` | automatic gzip request body decompression (**5MB limit after decompression**, guarding against decompression bombs) |
| `WithMaxConns(n)` | concurrent connection limit (503 + `Retry-After`) |
| `WithRateLimit(limiter, skipPaths...)` | rate limiting (429 + `Retry-After`; the limiter comes from the ratelimit package, see [ratelimit](./ratelimit.md)) |
| `WithJWT(j, getToken)` | JWT authentication (forwards to `jwt.AuthMiddleware`, see [jwt](./jwt.md)) |
| `WithCryption(key, skipPaths...)` | AES-GCM encryption/decryption of requests and responses: decrypts the request body (ciphertext limit 5MB by default, 413 beyond it); response bodies are **encrypted only for 2xx (and not 204/205/HEAD)**, while non-2xx such as errors and redirects pass through in plain text keeping their status code; a response exceeding the buffer limit (5MB by default) automatically falls back to plain text and **still writes the complete body** (never truncated). The request/response limits can be adjusted with `middleware.NewCryptionWithLimit(key, reqBytes, respBytes, skipPaths...)` |
| `WithContentSecurity(key, tolerance)` | content security validation (tamper and replay protection); **5MB request body limit** (413 beyond it, 400 on a read failure); 401 + `WWW-Authenticate` on an authentication failure |

### Status codes and headers follow the HTTP spec

The error responses produced by the middleware comply with the following RFC requirements:

| Status | Scenario (middleware) | Normative header | RFC reference |
|--------|--------------|-------------|---------|
| 401 | `WithJWT`, `WithContentSecurity` | `WWW-Authenticate` (**MUST**) | RFC 9110 §15.5.2 |
| 429 | `WithRateLimit` | `Retry-After` (MAY, emitted whenever possible) | RFC 6585 §4; RFC 9110 §10.2.3 |
| 503 | `WithBreaker`, `WithRouteBreaker`, `WithMaxConns` | `Retry-After` (**SHOULD**) | RFC 9110 §15.6.4 |
| 413 | `WithMaxBytes`, `WithCryption`, `WithContentSecurity` | — | RFC 9110 §15.5.14 |
| 405 | Routing layer (ServeMux) | `Allow` (MUST) | RFC 9110 §15.5.6 |

**401 challenge format**:

- JWT uses the Bearer scheme (RFC 6750 §3), with the `error` parameter distinguishing the failure reason:

  ```text
  WWW-Authenticate: Bearer error="invalid_request"   # no token provided
  WWW-Authenticate: Bearer error="invalid_token"     # invalid/expired token
  ```

- `ContentSecurity` is a custom HMAC signing scheme (it is neither Basic nor Bearer),
  so it uses a custom scheme name:

  ```text
  WWW-Authenticate: ContentSecurity
  ```

**Retry-After value rules** (RFC 9110 §10.2.3):

- Use `delay-seconds` (a non-negative decimal integer), **rounded up**.
- Anything under 1 second is still emitted as `1`, avoiding `Retry-After: 0` (which would be read as "retry immediately").
- **Omit the header when it cannot be estimated** (rather than making up a number) — a wrong wait hint makes
  clients wait far too long before retrying. That happens, for example, when a custom limiter doesn't
  implement `RetryAfterProvider`.

The rate limiting middleware asks the limiter for a precise value first (the optional `RetryAfterProvider`
interface), which every built-in `ratelimit` implementation supports:

| Limiter | Meaning of `RetryAfter()` |
|--------|------------------|
| `TokenBucket` | Time until the next token is available (computed from the live token count and the rate) |
| `SlidingWindow` | Time until the earliest record slides out of the window |
| `RedisTokenBucket` | Time needed to generate one token (estimated from the rate) |
| `RedisSlidingWindow` | The whole window duration (avoiding another Redis round trip on the rate-limited path) |
| `Concurrency` | Not implemented (the hold duration is unpredictable, so it must not be invented) |

When a custom limiter doesn't implement that interface, use `WithRetryAfter(d)` to provide a fixed value:

```go
mw := middleware.NewRateLimit(myLimiter).WithRetryAfter(2 * time.Second)
// the same applies to the breaker / concurrency limit
mb := middleware.NewBreaker().WithRetryAfter(time.Second)
mc := middleware.NewMaxConns(100).WithRetryAfter(500 * time.Millisecond)
```

> **`499` (client disconnected)**: written by `WithTimeout` when it detects that the request context was
> cancelled. It is not an RFC-defined status code but an nginx convention. It is kept because the response
> can no longer reach the disconnected client in that case, so the status code only serves server-side log
> observability. RFC 9110 §15 allows new status codes to be defined and 499 falls in the 4xx class, so it is
> a compliant extension; if your log pipeline doesn't recognize it, map it to another value in your own
> access-log middleware.

### Security-related default limits

The middleware below reads or expands the request body and all of them carry default limits to avoid exhausting memory:

| Middleware | Limit | Behavior beyond the limit | How to adjust |
|--------|------|---------|---------|
| `WithCryption` | 5MB ciphertext request body | 413 | `middleware.NewCryptionWithLimit(key, req, resp, ...)` |
| `WithContentSecurity` | 5MB signed request body | 413 (400 on a read failure) | `middleware.NewContentSecurity(key, tol).WithMaxBodyBytes(n)` |
| `WithGunzip` | 5MB **after decompression** | downstream reads return an error | `middleware.NewGunzip().WithMaxDecompressedBytes(n)` |
| `WithMaxBytes` | specified by the caller | 413 | `WithMaxBytes(n)` |

> `WithMaxBytes` relies on `Content-Length` and only limits the size of the **compressed** body, so it cannot
> stop decompression bombs; when combining it with `WithGunzip`, put `WithGunzip` on the inner side — the two
> complement each other.

### CORS, credentials and unauthorized origins

**Unauthorized origins pass through by default (no CORS headers emitted, the request continues downstream) rather than returning 403.**

CORS is a **browser-side** restriction on reading responses, not a server-side admission control: when
`Access-Control-Allow-Origin` is missing, the browser already blocks cross-origin scripts from reading the
response. Switching to 403 would instead:

- **Punish non-browser clients**: curl, mobile apps, service-to-service calls and some HTTP libraries attach
  an `Origin` header unconditionally; they aren't bound by CORS, yet a 403 would keep them out of the business logic.
- **Confuse two different concepts**: it expresses "this origin must not read the response" as "this request is forbidden".
- **Buy no extra safety**: the preflight would fail anyway (the browser never sends the real request), and even if
  the real request went through, its response still couldn't be read by a cross-origin script.

When you do need "only serve whitelisted origins" semantics, enable strict mode explicitly:

```go
// default: pass through (recommended)
server.Use(httpx.WithCors("https://app.example.com"))

// strict: unauthorized origins → 403 (for backends known to serve only whitelisted clients)
mw := middleware.NewCORS("https://app.example.com").WithRejectUnauthorizedOrigin(true)
server.Use(httpx.AsMiddleware(mw.Middleware()))
```

> ⚠️ **Don't use CORS as CSRF protection**. Simple requests (form POSTs, img GETs) aren't blocked by CORS in the
> first place, and cross-site requests still reach the server (it's only the response that can't be read). For
> state-changing endpoints use a CSRF token or a `SameSite` cookie.

Per the Fetch spec, wildcard origins are not allowed together with credentials. That's why `WithCors("*")`
**echoes the request's concrete Origin** (adding `Vary: Origin`) instead of sending `*` — otherwise the browser
would reject the whole response and any cross-origin request with `withCredentials` would inevitably fail.

> ⚠️ Allowing every origin *and* credentials means **any site** can issue a credentialed cross-origin request
> and read the response. Use an explicit origin list in production; if cookies/Authorization really aren't
> needed, disable credential emission with `middleware.NewCORS("*").WithCredentials(false)`.

### Per-route middleware and route patterns

"Per-route isolation" middleware such as `WithRouteBreaker` needs a **route pattern** as its key rather
than a concrete path: if `/users/1` and `/users/2` each got their own breaker, "per-route isolation"
would degrade into "per-request isolation" (fragmented statistics that never reach the breaker
threshold), and the breaker registry would grow without bound with the number of path parameters.

httpx resolves the pattern before entering the middleware chain and writes it into the context, where
it can be read with `middleware.PatternFromContext(ctx)`:

```go
import "github.com/chihqiang/infra-go/httpx/middleware"

pattern := middleware.PatternFromContext(r.Context())
// "GET /users/{id}" when it matches /users/{id}; an empty string when no route matched
```

> **Why not read `r.Pattern` directly inside the middleware**: global middleware wraps the outside of the
> `ServeMux`, and `net/http` only fills in `r.Pattern` when it dispatches the request to the matching
> handler, so `r.Pattern` is always empty in global middleware. httpx resolves it up front with
> `mux.Handler(r)` and passes it along.
>
> Other frameworks can call `middleware.ContextWithPattern(ctx, pattern)` to provide the same information.

`RouteBreaker`'s key resolution priority: `r.Pattern` → the pattern in the context → a normalized path
(replacing ID-like segments with `{}`, e.g. `/users/123` → `/users/{}`), which keeps the cardinality
bounded even outside ServeMux.

The breaker registry (`breaker.GetBreaker`) **caches by name forever and never evicts automatically**,
so the name cardinality must be bounded. `breaker.RegistrySize()` is available for observability and
`breaker.RemoveBreaker(name)` releases names that are no longer used.

Usage:

```go
server.Use(httpx.WithRecovery(), httpx.WithRequestID(), httpx.WithLogger("/healthz"))
server.Use(httpx.WithCors("*"))
server.Use(httpx.WithTracing("/health*", "/metrics/*"))   // put first so logs carry trace_id
server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))
server.Use(httpx.WithJWT(j, func(r *http.Request) string {
    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}))
server.Use(httpx.WithTimeout(5 * time.Second))
```

### Using the httpx/middleware subpackage directly (other frameworks / the standard library)

```go
import "github.com/chihqiang/infra-go/httpx/middleware"

// standard net/http
handler := middleware.NewRecovery().Middleware()(
    middleware.NewRequestID().Middleware()(mux),
)
http.ListenAndServe(":8080", handler)

// gin: hook it up with WrapH
router.Use(gin.WrapH(middleware.NewCORS("*").Middleware()(router)))
```

Error response mechanism: `middleware.WriteError(ctx, w, status, msg)` (exported). The render function is resolved in this order:

1. **Carried by the request context** (`middleware.ContextWithErrorHandler`) — the httpx `Server` injects the unified JSON renderer (carrying `request_id`) into every request, keeping httpx's format for requests dispatched through httpx;
2. Otherwise it falls back to the **process-wide global** (`middleware.SetErrorHandler`), which defaults to plain-text `http.Error`.

> Why not inject the global from `init()`: that would mean "merely importing httpx" silently changes the
> error response format of gin/echo routes in the same process (they use the `middleware` subpackage too),
> and since imports are order-independent there would be no way to opt out.
> With per-request injection, httpx only affects the requests it dispatches itself; components such as jwt
> that call `middleware.WriteError` / `WriteUnauthorized` with the request context inherit that format
> automatically as well.
>
> When you don't dispatch through httpx but want the process-wide behavior (e.g. using the subpackage
> directly with gin/echo), call `middleware.SetErrorHandler(fn)` explicitly. To reuse the httpx format on a
> custom `http.Server`, inject it yourself with `middleware.ContextWithErrorHandler(ctx, fn)`.

### Integrating custom / third-party standard middleware into httpx

`httpx.AsMiddleware(mw)` adapts any standard `func(http.Handler) http.Handler` middleware into an `httpx.Middleware`:

```go
server.Use(httpx.AsMiddleware(myStdMiddleware))                            // custom/third-party
server.Use(httpx.AsMiddleware(middleware.NewCORS("*").Middleware()))       // subpackage OO form
```

### skipPaths / ignorePaths matching

The `skipPaths` of `WithLogger` / `WithRateLimit` / `WithCryption` and the `ignorePaths` of `WithTracing` use the `PathMatcher` from the `httpx/x` subpackage, supporting exact matches (`/health`), prefix wildcards (`/health*`, crossing directories) and globs (`/api/*/x`, not crossing directories). See [httpx-x](./httpx-x.md).

## Built-in routes: PprofRoutes

```go
server.AddRoutes(httpx.PprofRoutes(""))                 // default prefix /debug/pprof
server.AddRoutes(httpx.PprofRoutes(""), httpx.WithMiddleware(authMW)) // add auth in production
```

## Server configuration

```go
server := httpx.NewServer(httpx.ServerConfig{
    Host: "0.0.0.0", Port: 8080,
    ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
    IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20,
    ShutdownTimeout: 10 * time.Second,
})
```

Or override programmatically with `RunOption`:

```go
httpx.NewServer(httpx.ServerConfig{...},
    httpx.WithReadTimeout(10*time.Second),
    httpx.WithWriteTimeout(10*time.Second),
    httpx.WithIdleTimeout(120*time.Second),
    httpx.WithMaxHeaderBytes(1<<20),
)
```

## Appendix

### Custom validator

```go
import "github.com/chihqiang/infra-go/httpx/binding"

type myValidator struct{}

func (v *myValidator) ValidateStruct(obj any) error { return nil }
func (v *myValidator) Engine() any                  { return nil }

binding.SetValidateFn((&myValidator{}).ValidateStruct) // plug in custom validation
binding.SetValidateFn(nil)                             // restore the default (go-playground/validator)
```

### Using binders directly (httpx/binding)

```go
import "github.com/chihqiang/infra-go/httpx/binding"

var obj MyReq
_ = binding.JSON.BindBody([]byte(`{...}`), &obj)   // bind from raw bytes
b := binding.Default(r.Method, r.Header.Get("Content-Type")) // binder selection
```

### Common middleware wrapping example

```go
// a handler wrapping "binding + validation + permissions" (for mounting on a route)
func authzMW(roles ...string) httpx.Middleware {
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            claims := jwt.ClaimsFromContext(r.Context())
            role, _ := claims[jwt.ClaimKeyRole].(string)
            if !slices.Contains(roles, role) {
                httpx.WriteHTTPErrorCtx(r.Context(), w, http.StatusForbidden, "forbidden")
                return
            }
            next(w, r)
        }
    }
}
```
