package httpx

import (
	"context"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/logger"
)

// This file provides request_id context helpers:
// ContextWithRequestID / RequestIDFromContext, and wires the logger context field
// extractor in init.
//
// The request_id context key and its accessors live in the httpx/middleware subpackage
// (the RequestID middleware injects it, the unified JSON response reads it, and the
// helpers in this package share the same key); this file only delegates and hooks into
// the logger.

func init() {
	logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
		ri := RequestIDFromContext(ctx)
		if ri == "" {
			return nil
		}
		return []logger.Field{
			logger.String("request_id", ri),
		}
	})
}

// ContextWithRequestID injects a request_id into the context.
// Used together with RequestIDFromContext:
//
//	ctx := httpx.ContextWithRequestID(r.Context(), "req-123")
//	resp := httpx.OkJSONCtx(ctx, w, data)
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return middleware.ContextWithRequestID(ctx, id)
}

// RequestIDFromContext extracts the request_id from the context; it returns an
// empty string when none is present.
func RequestIDFromContext(ctx context.Context) string {
	return middleware.RequestIDFromContext(ctx)
}
