package rest

import (
	"net/http"
	"net/netip"

	"github.com/chihqiang/infra-go/rest/internal/binding"
	"github.com/chihqiang/infra-go/rest/internal/realip"
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

// QueryParam returns the first query value named name. It returns an empty
// string when the request, URL, or value is absent.
func QueryParam(r *http.Request, name string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Query().Get(name)
}

// ClientIP returns the direct peer address of r. Forwarding headers are ignored
// because they can be spoofed by the client; use ClientIPTrusted behind a proxy
// or ClientIPFromHeaders when the headers come from a trusted network.
func ClientIP(r *http.Request) string {
	return realip.FromRequestTrusted(r, nil)
}

// ClientIPFromHeaders is the legacy behaviour: it trusts X-Forwarded-For and
// X-Real-IP from any peer. Only use it when every caller is a trusted proxy.
func ClientIPFromHeaders(r *http.Request) string {
	return realip.FromRequest(r)
}

// ClientIPTrusted returns the client address of r, honouring X-Forwarded-For
// and X-Real-IP only when the direct peer is inside trusted. An empty trusted
// list ignores the headers.
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
