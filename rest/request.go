package rest

import (
	"net/http"
	"net/netip"

	"github.com/chihqiang/ten-go/core/cast"
	"github.com/chihqiang/ten-go/rest/internal/binding"
	"github.com/chihqiang/ten-go/rest/internal/realip"
)

// Bind decodes the request into dst with the binder matching its content type,
// then validates it with the "binding" struct tags and, when implemented,
// dst.Validate. Requests without a body are read from the query string. A JSON
// body is also detected when Content-Type is missing or is the urlencoded form
// type; application/*+json media types are accepted as JSON.
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

// PathParam returns the path wildcard value named name, such as "id" for
// "/groups/{id}". It returns an empty string when the request or value is absent.
func PathParam(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	return r.PathValue(name)
}

// PathParamValue is the cast.Value form of PathParam, for callers that want the
// typed conversions (Int64, Uint64, Float64, Bool) or the emptiness check
// instead of the raw string. The Or variants of those conversions take a default
// that is used when the value is empty or cannot be converted.
//
// The value is empty (see cast.Value.IsEmpty) when the request is nil or the
// wildcard is absent; it is nil in the former case and the empty string in the
// latter.
func PathParamValue(r *http.Request, name string) cast.Value {
	if r == nil {
		return cast.NewValue(nil)
	}
	return cast.NewValue(r.PathValue(name))
}

// QueryParam returns the first query value named name. It returns an empty
// string when the request, URL, or value is absent.
func QueryParam(r *http.Request, name string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Query().Get(name)
}

// QueryParamValue is the cast.Value form of QueryParam, for callers that want
// the typed conversions (Int64, Uint64, Float64, Bool) or the emptiness check
// instead of the raw string. The Or variants of those conversions take a default
// that is used when the value is empty or cannot be converted.
//
// The value is empty (see cast.Value.IsEmpty) when the request or URL is nil,
// or when the parameter is absent.
func QueryParamValue(r *http.Request, name string) cast.Value {
	if r == nil || r.URL == nil {
		return cast.NewValue(nil)
	}
	return cast.NewValue(r.URL.Query().Get(name))
}

// ClientIP returns the direct peer address of r. Forwarding headers are ignored
// because they can be spoofed by the client; use ClientIPTrusted behind a proxy.
func ClientIP(r *http.Request) string {
	return realip.FromRequestTrusted(r, nil)
}

// ClientIPTrusted returns the client address of r, honouring X-Forwarded-For
// and X-Real-IP only when the direct peer is inside trusted. An empty trusted
// list ignores the headers, which makes it behave like ClientIP.
//
// The trusted list can be built with ParseTrustedProxies or supplied through
// Config.TrustedProxies.
func ClientIPTrusted(r *http.Request, trusted []netip.Prefix) string {
	return realip.FromRequestTrusted(r, trusted)
}

// ClientIPFromHeaders is the legacy behaviour: it trusts X-Forwarded-For and
// X-Real-IP from any peer. Only use it when every caller is a trusted proxy.
func ClientIPFromHeaders(r *http.Request) string {
	return realip.FromRequest(r)
}

// ParseTrustedProxies parses IP and CIDR strings into the trusted proxy
// prefixes accepted by ClientIPTrusted.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	return realip.ParseTrustedProxies(values)
}
