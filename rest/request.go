package rest

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/internal/binding"
	"github.com/chihqiang/infra-go/rest/internal/realip"
)

// Bind decodes the request into dst with the binder matching its content type,
// then validates it with the "binding" struct tags and, when implemented,
// dst.Validate. Requests without a body are read from the query string.
func Bind(r *http.Request, dst any) error {
	return binding.Bind(r, dst)
}

// BindQuery decodes the query string into dst using form tags (json tags are
// used as a fallback).
func BindQuery(r *http.Request, dst any) error {
	return binding.BindQuery(r, dst)
}

// BindHeader decodes request headers into dst using header tags.
func BindHeader(r *http.Request, dst any) error {
	return binding.BindHeader(r, dst)
}

// BindPath decodes path parameters into dst using path tags.
func BindPath(r *http.Request, dst any) error {
	return binding.BindPath(r, dst)
}

// ClientIP returns the client address of r, honouring X-Forwarded-For and
// X-Real-IP when they hold a valid address. The headers are trusted from any
// peer; use ClientIPTrusted when r may come directly from clients.
func ClientIP(r *http.Request) string {
	return realip.FromRequest(r)
}

// ClientIPTrusted returns the client address of r, honouring X-Forwarded-For
// and X-Real-IP only when the direct peer is inside trusted. An empty trusted
// list keeps the legacy behaviour of ClientIP.
//
// The trusted list can be built with ParseTrustedProxies or supplied through
// Config.TrustedProxies.
func ClientIPTrusted(r *http.Request, trusted []netip.Prefix) string {
	return realip.FromRequestTrusted(r, trusted)
}

// ParseTrustedProxies parses IP and CIDR strings for ClientIPTrusted.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	return realip.ParseTrustedProxies(values)
}

// RequestID returns the ID assigned by handler.RequestIDHandler, or an empty
// string when the middleware is not part of the chain.
func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return RequestIDFromContext(r.Context())
}

// RequestIDFromContext returns the request ID stored in ctx by
// handler.RequestIDHandler, or an empty string when absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := handler.RequestIDFromContext(ctx)
	return id
}
