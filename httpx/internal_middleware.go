package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/jwt"
)

// This file provides the httpx adapter layer for the built-in HTTP middleware
// (it forwards to the httpx/middleware subpackage).
// The core logic lives in the httpx/middleware subpackage: one file per middleware,
// object-oriented (NewXxx + Middleware()), returning the standard
// func(http.Handler) http.Handler shape, with no dependency on httpx so it can be
// reused by other net/http-compatible frameworks such as gin / echo.
//
// This file only adapts the standard middleware to httpx.Middleware
// (func(http.HandlerFunc) http.HandlerFunc) so it can be registered via
// server.Use / WithMiddleware / ApplyMiddleware; the method signatures are unchanged.
//
// Middleware inventory: CORS (WithCors), Recovery, RequestID, tracing (WithTracing),
// access logging, circuit breaking (global/per-route), timeout, request body size limit,
// gzip decompression, concurrent connection limit, rate limiting (WithRateLimit),
// JWT authentication (WithJWT, forwarding to jwt.AuthMiddleware), request/response
// encryption, and content security verification.

// middlewareErrorHandler writes errors produced by the httpx/middleware subpackage
// using the unified httpx response format.
//
// Why not inject a global handler from init: that would make merely importing httpx
// silently change the error response format of gin/echo routes in the same process
// (they use the httpx/middleware subpackage too), and since imports are independent of
// call order, users could not opt out.
// Instead the Server injects it into each request's context (see buildGlobalHandler in
// server.go), scoping it to requests dispatched through httpx; components such as jwt
// that call middleware.WriteError / WriteUnauthorized via the request context
// automatically inherit this format.
func middlewareErrorHandler(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	WriteHTTPErrorCtx(ctx, w, status, msg)
}

// WithCors returns a middleware that sets CORS headers on responses.
//
// allowOrigins is the list of allowed origins; passing "*" allows all origins.
// Same-origin requests (Origin matching Host) get no CORS headers;
// unauthorized origins get 403; OPTIONS preflight requests get 204.
//
//	server.Use(httpx.WithCors("*"))
//	server.Use(httpx.WithCors("http://a.com", "http://b.com"))
func WithCors(allowOrigins ...string) Middleware {
	return AsMiddleware(middleware.NewCORS(allowOrigins...).Middleware())
}

// WithRecovery returns a panic recovery middleware.
// It catches panics in handlers, logs the stack trace and returns 500, preventing
// the process from crashing.
//
//	server.Use(httpx.WithRecovery())
func WithRecovery() Middleware {
	return AsMiddleware(middleware.NewRecovery().Middleware())
}

// WithRequestID returns a request_id middleware.
// It reads the X-Request-Id request header and generates one (google/uuid) when
// absent, injects it into the context and writes it back to the X-Request-Id
// response header.
//
//	server.Use(httpx.WithRequestID())
//
// Used together with OkJSONCtx / OkXMLCtx / WriteHTTPErrorCtx, the request_id
// automatically appears in the response.
func WithRequestID() Middleware {
	return AsMiddleware(middleware.NewRequestID().Middleware())
}

// WithTracing returns an HTTP server-side tracing middleware (forwards to
// middleware.NewTracing).
//
// Features:
//   - Extracts the upstream-propagated span context (W3C traceparent) from headers
//   - Creates a server span per request (carrying HTTP semantics such as
//     method/path/status)
//   - Injects the span into the context so downstream modules such as
//     logger/orm/redisx can correlate trace_id automatically
//
// It uses the global TracerProvider by default (assembled via trace.StartAgent);
// when the request context already holds a valid span, that span's TracerProvider
// is reused (supporting nested tracing).
//
// ignorePaths is the list of paths not to trace (health checks, probes, etc.);
// matching paths pass straight through without creating a span.
// Matching works the same as WithLogger: exact match (e.g. "/health") or a prefix
// wildcard ending in "*" (e.g. "/health*" matches /health, /healthz, /health/live).
//
//	server.Use(httpx.WithTracing())                    // trace all requests
//	server.Use(httpx.WithTracing("/health*", "/metrics/*")) // skip probes
func WithTracing(ignorePaths ...string) Middleware {
	return AsMiddleware(middleware.NewTracing(ignorePaths...).Middleware())
}

// WithLogger returns a request logging middleware.
// It records the method, path, status code, response byte count and latency of
// every request.
// When used with the trace package, the logger's Ctx extractor automatically
// includes trace_id/span_id.
//
// skipPaths is the list of paths not to log, commonly used for high-frequency probe
// endpoints such as health checks and heartbeats.
// Two matching modes are supported:
//
//   - Exact match: e.g. "/healthz", matching only that path;
//
//   - Prefix wildcard: ending in "*", e.g. "/internal/*", matching every path with
//     that prefix.
//
//     server.Use(httpx.WithLogger())                       // log all requests
//     server.Use(httpx.WithLogger("/healthz", "/metrics")) // exact skip
//     server.Use(httpx.WithLogger("/internal/*"))          // prefix wildcard skip
func WithLogger(skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewAccessLogger(skipPaths...).Middleware())
}

// WithBreaker returns a circuit breaking middleware that protects downstream
// handlers from cascading failure.
// It is based on the breaker module's Google SRE algorithm, and all requests share a
// single breaker instance; use WithRouteBreaker if you need per-route isolation.
//
// When the circuit is open it returns 503 Service Unavailable; successful requests
// (<500) report Accept and failed requests (>=500) report Reject, driving the
// breaker state.
//
//	server.Use(httpx.WithBreaker())
func WithBreaker() Middleware {
	return AsMiddleware(middleware.NewBreaker().Middleware())
}

// WithRouteBreaker returns a per-route isolated circuit breaking middleware.
// Each route (METHOD:path) has its own breaker so statistics do not interfere,
// preventing failures in one route from lowering the pass rate of others.
//
// Breakers are cached by name via breaker.GetBreaker, so routes with the same name
// share one instance.
// When the circuit is open it returns 503 Service Unavailable; successful requests
// (<500) report Accept and failed requests (>=500) report Reject.
//
//	server.Use(httpx.WithRouteBreaker())
func WithRouteBreaker() Middleware {
	return AsMiddleware(middleware.NewRouteBreaker().Middleware())
}

// WithTimeout returns a request timeout middleware.
// Each request may run for at most duration; on timeout it returns
// 503 Service Unavailable.
// A client-initiated disconnect returns 499; WebSocket / SSE requests are not
// subject to the timeout.
//
//	When duration <= 0 the middleware is a no-op (passes through).
//
//	server.Use(httpx.WithTimeout(5 * time.Second))
func WithTimeout(duration time.Duration) Middleware {
	return AsMiddleware(middleware.NewTimeout(duration).Middleware())
}

// WithMaxBytes returns a middleware that limits the request body size.
// When the request body Content-Length exceeds n bytes it immediately returns
// 413 Request Entity Too Large.
// For chunked transfers (no Content-Length), http.MaxBytesReader enforces the limit
// while reading.
//
//	n <= 0 means no limit.
//
//	server.Use(httpx.WithMaxBytes(1 << 20)) // limit to 1MB
func WithMaxBytes(n int64) Middleware {
	return AsMiddleware(middleware.NewMaxBytes(n).Middleware())
}

// WithGunzip returns a middleware that automatically decompresses gzip request
// bodies.
// When the Content-Encoding header contains "gzip", the request body is wrapped in a
// gzip reader.
// A decompression failure returns 400 Bad Request.
//
//	server.Use(httpx.WithGunzip())
func WithGunzip() Middleware {
	return AsMiddleware(middleware.NewGunzip().Middleware())
}

// WithMaxConns returns a middleware that limits the number of concurrently handled
// requests.
// When concurrency exceeds n it immediately returns 503 Service Unavailable,
// preventing connection exhaustion.
//
//	n <= 0 means no limit.
//
//	server.Use(httpx.WithMaxConns(1000))
func WithMaxConns(n int) Middleware {
	return AsMiddleware(middleware.NewMaxConns(n).Middleware())
}

// WithRateLimit returns an HTTP rate limiting middleware backed by a limiter
// (forwards to middleware.NewRateLimit).
//
// Each request first asks the limiter for a quota and passes when allowed; throttled
// requests get 429 Too Many Requests.
// The limiter comes from the ratelimit package (ratelimit.NewTokenBucket /
// NewSlidingWindow / Redis limiters, etc.); its method set matches
// middleware.RateLimiter so it can be passed directly; nil degrades to no rate
// limiting (fail-open).
//
// skipPaths is the list of paths exempt from rate limiting; matching paths pass
// straight through (commonly used for high-frequency probe endpoints such as health
// checks).
// Matching works the same as WithLogger: exact match (e.g. "/healthz") or a prefix
// wildcard ending in "*" (e.g. "/internal/*").
//
//	server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))          // 100/s, burst 200
//	server.Use(httpx.WithRateLimit(ratelimit.NewSlidingWindow(10, time.Minute))) // 10 per minute
//	server.Use(httpx.WithRateLimit(redisLimiter, "/healthz", "/metrics"))      // skip probe endpoints
func WithRateLimit(limiter middleware.RateLimiter, skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewRateLimit(limiter, skipPaths...).Middleware())
}

// WithCryption returns an AES-GCM request/response encryption middleware.
// The request body must be base64-encoded AES-GCM ciphertext (nonce || ciphertext);
// the middleware decrypts it and hands it to the handler (chunked requests are
// supported; ciphertext defaults to a 4MB cap and returns 413 when exceeded).
// The response body is **encrypted only for 2xx (excluding 204/205 and HEAD)**;
// non-2xx outcomes such as errors and redirects, plus bodyless responses, pass
// through in plaintext keeping the original status code, which eases client
// debugging and preserves correct HTTP semantics.
//
// It uses AES-GCM authenticated encryption (AEAD), guaranteeing both confidentiality
// and integrity (tamper resistance), with a fresh random nonce per message; responses
// larger than 1MB automatically fall back to plaintext (unencrypted) output to avoid
// OOM from large responses. The key must be 16/24/32 bytes long (matching
// AES-128/192/256).
//
// skipPaths is the list of paths exempt from request/response encryption; matching
// paths pass through in plaintext (commonly used for callbacks, static assets and
// other cases that cannot be encrypted). Matching works the same as WithLogger:
// exact match (e.g. "/callback") or a prefix wildcard ending in "*"
// (e.g. "/public/*").
//
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef")))               // encrypt everything
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef"), "/callback"))  // exact skip
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef"), "/public/*"))  // prefix wildcard skip
func WithCryption(key []byte, skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewCryption(key, skipPaths...).Middleware())
}

// WithContentSecurity returns a content security verification middleware
// (tamper-proof + replay-proof).
// The client must carry a signature in the `X-Content-Security` header:
//
//	X-Content-Security: time=<unix seconds>; signature=<base64 HMAC-SHA256>
//
// The signed content is: `timestamp\nmethod\npath\nquery\nbodySha256Hex`
// (timestamp is the timestamp from the request header, bodySha256Hex is the SHA-256
// hex digest of the request body).
//
// Verification rules:
//   - Valid signature (HMAC-SHA256 match) and timestamp within the tolerance window
//     → pass
//   - Invalid signature → 401
//   - Timestamp outside the tolerance (replay protection) → 403
//
// key is the HMAC secret shared by both parties.
//
//	server.Use(httpx.WithContentSecurity([]byte("shared-secret"), 5*time.Minute))
func WithContentSecurity(key []byte, tolerance time.Duration) Middleware {
	return AsMiddleware(middleware.NewContentSecurity(key, tolerance).Middleware())
}

// WithJWT returns a JWT-based HTTP authentication middleware (forwards to
// jwt.JWT.AuthMiddleware).
//
// j is *jwt.JWT; getToken is supplied by the caller and extracts the token from the
// request (e.g. from a Header/Cookie/Query), so the middleware only parses, verifies
// and injects business claims and does not care where the token comes from.
// A failed verification returns 401; on success the business claims (excluding
// standard claims and token_type) are injected into the context and read by
// downstream handlers via jwt.ClaimsFromContext.
// Error responses are emitted through the unified httpx/middleware error mechanism
// (i.e. this package's unified JSON response).
//
//	j := jwt.MustNew(jwt.Config{Secret: "..."})
//	server.Use(httpx.WithJWT(j, func(r *http.Request) string {
//	    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
//	}))
func WithJWT(j *jwt.JWT, getToken func(*http.Request) string) Middleware {
	return j.AuthMiddleware(getToken)
}
