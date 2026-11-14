package router

import (
	"net/http"
	"path"
	"strings"
	"sync"
)

// Router provides a small route-registration boundary around http.ServeMux.
type Router struct {
	mux                     *http.ServeMux
	fallbackMu              sync.RWMutex
	notFoundHandler         http.Handler
	methodNotAllowedHandler http.Handler
}

// New creates a router backed by Go's method-aware ServeMux.
func New() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Handle registers a handler using the method and path pattern.
func (r *Router) Handle(method, routePath string, handler http.Handler) {
	r.mux.Handle(Pattern(method, routePath), handler)
}

// ServeHTTP dispatches a request to its registered route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.fallbackMu.RLock()
	notFoundHandler := r.notFoundHandler
	methodNotAllowedHandler := r.methodNotAllowedHandler
	r.fallbackMu.RUnlock()
	if notFoundHandler != nil || methodNotAllowedHandler != nil {
		switch r.fallbackStatus(req) {
		case http.StatusNotFound:
			if notFoundHandler != nil {
				notFoundHandler.ServeHTTP(w, req)
				return
			}
		case http.StatusMethodNotAllowed:
			if methodNotAllowedHandler != nil {
				methodNotAllowedHandler.ServeHTTP(w, req)
				return
			}
		}
	}
	r.mux.ServeHTTP(w, req)
}

// SetNotFoundHandler sets the handler used when no route matches the request.
func (r *Router) SetNotFoundHandler(handler http.Handler) {
	r.fallbackMu.Lock()
	r.notFoundHandler = handler
	r.fallbackMu.Unlock()
}

// SetNotAllowedHandler sets the handler used when a path matches but the method does not.
func (r *Router) SetNotAllowedHandler(handler http.Handler) {
	r.fallbackMu.Lock()
	r.methodNotAllowedHandler = handler
	r.fallbackMu.Unlock()
}

// Mux returns the underlying ServeMux for advanced registration.
func (r *Router) Mux() *http.ServeMux {
	return r.mux
}

// MatchedPattern reports the registered pattern matching req, or an empty string.
func (r *Router) MatchedPattern(req *http.Request) string {
	_, pattern := r.mux.Handler(req)
	return pattern
}

// IsNotFound reports whether req resolves to the mux's default 404 handler.
// A method mismatch is not considered a 404, preserving ServeMux's 405 response.
func (r *Router) IsNotFound(req *http.Request) bool {
	return r.fallbackStatus(req) == http.StatusNotFound
}

// IsMethodNotAllowed reports whether req matches a route path but not its method.
func (r *Router) IsMethodNotAllowed(req *http.Request) bool {
	return r.fallbackStatus(req) == http.StatusMethodNotAllowed
}

func (r *Router) fallbackStatus(req *http.Request) int {
	handler, pattern := r.mux.Handler(req)
	if pattern != "" {
		return 0
	}
	recorder := statusRecorder{}
	handler.ServeHTTP(&recorder, req)
	return recorder.status
}

type statusRecorder struct {
	header http.Header
	status int
}

func (r *statusRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(p), nil
}

// Pattern builds a method-aware ServeMux pattern and normalizes its path.
func Pattern(method, routePath string) string {
	method = strings.ToUpper(method)
	routePath = NormalizePath(routePath)
	if method == "" || method == "*" {
		return routePath
	}
	return method + " " + routePath
}

// NormalizePath ensures a non-empty path begins with a slash.
func NormalizePath(routePath string) string {
	if routePath == "" {
		return "/"
	}
	if !strings.HasPrefix(routePath, "/") {
		return "/" + routePath
	}
	return routePath
}

// JoinPath joins a route prefix and path while preserving subtree trailing slashes.
func JoinPath(prefix, routePath string) string {
	if prefix == "" {
		return NormalizePath(routePath)
	}
	joined := path.Join(prefix, routePath)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if strings.HasSuffix(routePath, "/") && routePath != "/" && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
