# httpx/x

A collection of general-purpose HTTP utilities for `httpx`, living in the `httpx/x` subpackage and reused by the `httpx/middleware` middlewares and by business code. Miscellaneous helpers unrelated to the middlewares themselves are consolidated here on demand, avoiding a dedicated package for every single function.

Currently contains:

- **Path matcher `PathMatcher`**: the shared implementation behind the middleware skip / ignore rules (`WithLogger` / `WithCryption` / `WithRateLimit` / `WithTracing`);
- **Client IP resolution `ClientIP` / `IPChecker`**: obtain the real client IP after reverse-proxy forwarding.

```go
import "github.com/chihqiang/infra-go/httpx/x"
```

## PathMatcher path matching

```go
m := x.NewPathMatcher([]string{"/health", "/health*", "/api/*/x"})
m.Match("/healthz") // true
```

### Rule semantics

Each rule supports three forms:

| Form | Example | Description |
|------|------|------|
| Exact match | `/health` | Matches that path only |
| Prefix wildcard | `/health*` | Ends with `*`; matches any path starting with that prefix (**may cross directories** — `/health/live` matches too) |
| glob wildcard | `/api/*/x`, `/v[0-9]/info` | Based on `path.Match`; `*` does **not** cross directories; supports `?` and `[...]` |

> **Note**: rules ending in `*` match by "prefix" (and may cross directories), so `/metrics/*` also matches `/metrics/a/b`; if you only want to match one level of sub-path, use a glob rule without the trailing `*`, such as `/metrics/?` or `/*/foo`.

Empty-string rules are ignored; passing no rules matches no path.

`httpx.With*` middleware entry points already accept `...string` path arguments and call `x.NewPathMatcher` internally, so business code never needs this package directly; to define custom matching rules (e.g. a gateway auth whitelist), use `NewPathMatcher` + `Match` directly.

## ClientIP client IP resolution

Targeted at services "deployed behind a trusted reverse proxy"; obtains the real client IP (plain IP, no port), and works out of the box for common proxy setups (Nginx / CDN / cloud LB).

**Main package convenience entry (recommended, most common in business code)**: the `httpx` main package re-exports it in `httpx/request.go`, so simply call `httpx.ClientIP(r)` without importing the subpackage:

```go
import "github.com/chihqiang/infra-go/httpx"

ip := httpx.ClientIP(r)                          // default: loopback/private ranges treated as trusted proxies
ip := httpx.ClientIPWithTrustedProxies(r, "100.64.0.0/10") // add trusted CIDRs (cloud LB/CGNAT)
```

Subpackage entry (`x.ClientIP` / `x.NewIPChecker`, which middleware uses internally) for cases that need to reuse the resolver or import the subpackage directly:

```go
ip := x.ClientIP(r) // convenience: loopback/private ranges treated as trusted proxies by default
```

Secure resolution strategy: look at the direct peer `RemoteAddr` first — if untrusted (a client connecting directly over the public internet), proxy headers are ignored entirely and only the peer is returned; only when trusted (loopback/private/gateway) are proxy headers parsed. Proxy header order: vendor headers → `X-Forwarded-For` (walk from right to left skipping trusted proxies, preventing forged prefixes) → `Forwarded` (RFC 7239) → `X-Real-IP` → fall back to `RemoteAddr`.

When customisation is needed (traffic returns through a public CDN/WAF, or vendor headers such as Cloudflare are enabled), use a reusable resolver:

```go
ipc := x.NewIPChecker(
    x.WithTrustedProxies("100.64.0.0/10"),     // add trusted proxy CIDRs
    x.WithVendorHeaders(x.HeaderCFConnectingIP), // enable the Cloudflare header
)
ip := ipc.ClientIP(r)
```

Common header constants: `HeaderXForwardedFor`, `HeaderXRealIP`, `HeaderForwarded`, `HeaderCFConnectingIP`, `HeaderTrueClientIP`. See the `x` package source comments for details (resolution priority and trust model).
