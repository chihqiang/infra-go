package httpx

import (
	"errors"
	"net/http"

	"github.com/chihqiang/infra-go/cast"
	"github.com/chihqiang/infra-go/httpx/binding"
	"github.com/chihqiang/infra-go/httpx/x"
)

// This file gathers the convenience APIs for the HTTP request side:
//   - Struct binding: Bind*/MustBind* (map request data onto a struct and validate)
//   - Single value reads: QueryValue/PathValue/HeaderValue (read by key and convert type)
//   - Client IP: ClientIP / ClientIPWithTrustedProxies (real client IP, forwarding httpx/x)

// --- Binding functions ---

// Bind picks a binder automatically based on the request Method and Content-Type.
// GET requests use form binding (query parameters); other requests select by
// Content-Type.
func Bind(r *http.Request, obj any) error {
	return binding.Default(r.Method, r.Header.Get("Content-Type")).Bind(r, obj)
}

// BindJSON binds the request body as JSON into obj.
func BindJSON(r *http.Request, obj any) error {
	return binding.JSON.Bind(r, obj)
}

// BindXML binds the request body as XML into obj.
func BindXML(r *http.Request, obj any) error {
	return binding.XML.Bind(r, obj)
}

// BindQuery binds URL query parameters into obj.
// Fields are matched using the `form` tag.
func BindQuery(r *http.Request, obj any) error {
	return binding.Query.Bind(r, obj)
}

// BindForm binds form data (query + post form) into obj.
// Fields are matched using the `form` tag.
func BindForm(r *http.Request, obj any) error {
	return binding.Form.Bind(r, obj)
}

// BindHeader binds HTTP headers into obj.
// Fields are matched using the `header` tag.
func BindHeader(r *http.Request, obj any) error {
	return binding.Header.Bind(r, obj)
}

// BindURI binds URI path parameters into obj.
// params usually comes from the path parameters parsed by the router, e.g.
// {"id": "123"}.
// Fields are matched using the `uri` tag.
func BindURI(params map[string]string, obj any) error {
	m := make(map[string][]string, len(params))
	for k, v := range params {
		m[k] = []string{v}
	}
	return binding.Uri.BindUri(m, obj)
}

// BindURIWithValues binds path parameters in map[string][]string form into obj.
func BindURIWithValues(params map[string][]string, obj any) error {
	return binding.Uri.BindUri(params, obj)
}

// --- MustBind family (bind + automatically write an error response) ---

// MustBind binds and validates request data, writing an HTTP error response on
// failure.
// It returns nil on success, or the error after writing the response on failure.
func MustBind(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := Bind(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindJSON binds and validates JSON, writing an HTTP error response on failure.
func MustBindJSON(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindJSON(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindQuery binds and validates query parameters, writing an HTTP error response
// on failure.
func MustBindQuery(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindQuery(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindForm binds and validates form data, writing an HTTP error response on
// failure.
func MustBindForm(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindForm(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// --- Internal helpers ---

// writeBindError writes the appropriate HTTP response for a binding error kind.
// It uses WriteHTTPErrorCtx so the response carries the request_id from the request
// context.
func writeBindError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		WriteHTTPErrorCtx(r.Context(), w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		WriteHTTPErrorCtx(r.Context(), w, http.StatusBadRequest, err.Error())
	}
}

// --- Single value convenience helpers ---
//
// Generic helpers that read a single value from the request by key, suited to the
// "read a handful of keys directly" case; when there are many fields or you need
// validation/defaults, prefer the Bind* / MustBind* helpers above to bind onto a
// struct.

// valueOf converts a raw string to type T, reusing cast.ToE underneath; it supports
// string, int/uint/float of every width, bool, time.Duration and time.Time;
// it returns def when raw is empty or conversion fails.
func valueOf[T any](raw string, def T) T {
	if raw == "" {
		return def
	}
	v, err := cast.ToE[T](raw)
	if err != nil {
		return def
	}
	return v
}

// defValue returns the first of the optional default values; it returns the zero value
// of type T when no def is provided.
func defValue[T any](def []T) T {
	var zero T
	if len(def) > 0 {
		return def[0]
	}
	return zero
}

// --- URL query parameters ---

// QueryValue reads key from the URL query and converts it to type T.
// For example, for the request /users?tag=a, QueryValue[string](r, "tag") returns "a".
// It returns def when the key is missing, the value is empty or conversion fails;
// with no def provided it returns the zero value of T.
func QueryValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.URL.Query().Get(key)
	}
	return valueOf(raw, defValue(def))
}

// --- Path parameters ---

// PathValue reads key from the path parameters and converts it to type T.
// The route must use Go 1.22's {key} pattern, e.g. "/users/{id}".
// It returns def when the key is missing, the value is empty or conversion fails;
// with no def provided it returns the zero value of T.
func PathValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.PathValue(key)
	}
	return valueOf(raw, defValue(def))
}

// --- Request headers ---

// HeaderValue reads key from the request headers and converts it to type T.
// Header names are case-insensitive, e.g. HeaderValue(r, "X-Token", "").
// It returns def when the key is missing, the value is empty or conversion fails;
// with no def provided it returns the zero value of T.
func HeaderValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.Header.Get(key)
	}
	return valueOf(raw, defValue(def))
}

// --- Client IP ---

// ClientIP returns the real client IP of the request (a bare IP without port), the
// most common convenience entry point under the default rules:
//
//	ip := httpx.ClientIP(r)
//
// It reuses httpx/x's default IPChecker resolver underneath (loopback and private
// networks are treated as trusted proxies), recognises
// X-Forwarded-For / Forwarded (RFC 7239) / X-Real-IP and resists forged prefixes;
// it falls back to RemoteAddr for clients connecting directly from the public internet.
//
// It returns an empty string for a nil request or when the IP cannot be determined.
// If you need custom trusted proxy ranges (e.g. traffic coming back through a public
// CDN/WAF/cloud LB) or vendor headers (CF-Connecting-IP / True-Client-IP), use
// ClientIPWithTrustedProxies, or build an x.NewIPChecker yourself for reuse.
func ClientIP(r *http.Request) string {
	return x.ClientIP(r)
}

// ClientIPWithTrustedProxies returns the real client IP of the request after
// appending custom trusted proxy ranges to the default trusted ranges. It suits
// traffic returning through a public CDN/WAF/cloud LB, for example when their egress
// is in 100.64.0.0/10 (cloud vendor LB/CGNAT):
//
//	ip := httpx.ClientIPWithTrustedProxies(r, "100.64.0.0/10")
//
// If you need to reuse the resolver at high frequency (avoiding repeated CIDR
// parsing), use x.NewIPChecker(WithTrustedProxies(...)).
func ClientIPWithTrustedProxies(r *http.Request, trusted ...string) string {
	return x.ClientIPWithTrustedProxies(r, trusted...)
}
