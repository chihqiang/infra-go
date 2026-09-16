// Package middleware provides the shared core implementation of common HTTP
// middleware, reusable by httpx and other net/http-compatible frameworks.
//
// Design conventions:
//
//   - One middleware per file and one type per middleware; construct it with
//     NewXxx(...) (parameter precomputation happens at construction time to
//     avoid re-parsing on every request) and obtain the standard middleware
//     with (m *Xxx) Middleware();
//
//   - Middleware() returns the standard form func(http.Handler) http.Handler and
//     does not depend on any specific framework, so it works with plain
//     net/http, httpx, gin, echo and so on:
//
//     // plain net/http
//     handler := middleware.NewRecovery().Middleware()(mux)
//
//     // httpx (internal_middleware.go already ships With* helpers, just use server.Use)
//     server.Use(httpx.WithRecovery())
//
//     // gin
//     router.Use(gin.WrapH(middleware.NewRequestID().Middleware()(ginEngine)))
//
//   - Error responses are written through ErrorHandler (plain text via
//     http.Error by default).
//     The render function is resolved in this order: the one carried by the
//     **request context** (ContextWithErrorHandler) wins, otherwise it falls
//     back to the process-wide global (SetErrorHandler).
//     The Server in the httpx main package injects its own unified JSON renderer
//     on every request, so requests dispatched through httpx keep the httpx
//     response format, while routes served by other frameworks (gin/echo) in the
//     same process stay unaffected.
//     To change this package's default behaviour when there is no request scope,
//     inject one explicitly with SetErrorHandler.
package middleware

import (
	"context"
	"net/http"
	"sync"
)

// ErrorHandler writes error responses produced by the middleware (for example
// timeouts, rejections or decryption failures).
type ErrorHandler func(ctx context.Context, w http.ResponseWriter, status int, msg string)

// defaultErrorHandler is the default error response: plain text via http.Error
// (with no framework dependency).
func defaultErrorHandler(_ context.Context, w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

var (
	errorHandlerMu sync.RWMutex
	errorHandler   ErrorHandler = defaultErrorHandler
)

// SetErrorHandler replaces the global error response writer; a nil fn restores
// the default (http.Error).
//
// The global value only takes effect when the **request context carries no**
// render function (see ContextWithErrorHandler).
// The httpx main package no longer rewrites this global in init: it injects per
// request while the Server handles the request, so that merely importing httpx
// cannot silently change the error response format of gin/echo routes in the
// same process.
// Use this function to inject explicitly when a process-wide effect is wanted
// without a request scope (for example when using this package directly for
// gin/echo).
func SetErrorHandler(fn ErrorHandler) {
	errorHandlerMu.Lock()
	defer errorHandlerMu.Unlock()
	if fn == nil {
		errorHandler = defaultErrorHandler
		return
	}
	errorHandler = fn
}

// globalErrorHandler returns the current global error render function.
func globalErrorHandler() ErrorHandler {
	errorHandlerMu.RLock()
	defer errorHandlerMu.RUnlock()
	return errorHandler
}

// errorHandlerKey is the context key for the request-scoped error render function.
// A private empty struct is used so it cannot collide with keys of other packages.
type errorHandlerKey struct{}

// ContextWithErrorHandler returns a copy of ctx carrying the error render
// function fn.
//
// The error response format can therefore take effect at **request** scope
// instead of relying solely on process-wide global state.
// The httpx Server injects its own unified JSON renderer on every request, which
// keeps requests dispatched through httpx in the httpx format while leaving the
// routes of other frameworks in the same process unaffected.
// When fn is nil, ctx is returned unchanged.
func ContextWithErrorHandler(ctx context.Context, fn ErrorHandler) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, errorHandlerKey{}, fn)
}

// WriteError writes an error response using the currently effective
// ErrorHandler.
// The render function carried by ctx is preferred (which is the case for httpx
// requests); otherwise it falls back to the global one.
// It lets this package's middleware, as well as callers that do not directly
// depend on the httpx main package (such as jwt.AuthMiddleware), share the same
// error rendering.
func WriteError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	writeError(ctx, w, status, msg)
}

// writeError writes an error response using the currently effective ErrorHandler.
func writeError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	if ctx != nil {
		if fn, ok := ctx.Value(errorHandlerKey{}).(ErrorHandler); ok && fn != nil {
			fn(ctx, w, status, msg)
			return
		}
	}
	globalErrorHandler()(ctx, w, status, msg)
}
