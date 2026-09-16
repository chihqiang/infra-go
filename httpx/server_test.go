package httpx

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper functions ---

// newTestServer creates a Server for tests (it does not start an HTTP service, it only
// registers routes).
func newTestServer(opts ...RunOption) *Server {
	return NewServer(ServerConfig{Host: "0.0.0.0", Port: 0}, opts...)
}

// doRequest sends a request to the Server's Handler and returns the response.
func doRequest(t *testing.T, s *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// doRequestWithHeaders sends a request with custom headers.
func doRequestWithHeaders(t *testing.T, s *Server, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// --- Basic routing tests ---

func TestAddRoute(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/hello",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "hello") },
	})

	rec := doRequest(t, s, http.MethodGet, "/hello", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hello")
}

func TestAddRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/list", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "list") }},
		{Method: "POST", Path: "/create", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "created") }},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/list", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "list")

	rec2 := doRequest(t, s, http.MethodPost, "/create", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "created")
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/users",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	// POST to a GET route should return 405
	rec := doRequest(t, s, http.MethodPost, "/users", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestNotFound(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method:  "GET",
		Path:    "/users",
		Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	rec := doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- WithPrefix tests ---

func TestWithPrefix(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") }},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "created") }},
	}, WithPrefix("/api/v1"))

	// Prefixed routes should be reachable
	rec1 := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "users")

	rec2 := doRequest(t, s, http.MethodPost, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Routes without the prefix should 404
	rec3 := doRequest(t, s, http.MethodGet, "/users", nil)
	assert.Equal(t, http.StatusNotFound, rec3.Code)
}

func TestWithPrefix_EmptyPrefix(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") }},
	}, WithPrefix(""))

	rec := doRequest(t, s, http.MethodGet, "/ping", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "pong")
}

func TestWithPrefix_NestedParams(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, r.PathValue("id"))
		}},
	}, WithPrefix("/api/v1"))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/users/42", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "42")
}

// --- Middleware tests ---

// recordingMiddleware records middleware execution order.
func recordingMiddleware(name string, order *[]string) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name+"-before")
			next(w, r)
			*order = append(*order, name+"-after")
		}
	}
}

func TestMiddleware_WithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("mw1", &order)))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, []string{"mw1-before", "handler", "mw1-after"}, order)
}

func TestMiddleware_MultipleWithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddlewares(
		recordingMiddleware("mw1", &order),
		recordingMiddleware("mw2", &order),
	))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Execution order: mw1 → mw2 → handler
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

func TestMiddleware_GlobalUse(t *testing.T) {
	var order []string
	s := newTestServer()
	s.Use(recordingMiddleware("global", &order))
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("group", &order)))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Global middleware → group middleware → handler
	assert.Equal(t, []string{"global-before", "group-before", "handler", "group-after", "global-after"}, order)
}

func TestMiddleware_GlobalUseMultiple(t *testing.T) {
	var order []string
	s := newTestServer()
	s.Use(
		recordingMiddleware("g1", &order),
		recordingMiddleware("g2", &order),
	)
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddlewares(
		recordingMiddleware("grp1", &order),
		recordingMiddleware("grp2", &order),
	))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Global middleware runs first → group middleware → handler
	assert.Equal(t, []string{
		"g1-before", "g2-before",
		"grp1-before", "grp2-before",
		"handler",
		"grp2-after", "grp1-after",
		"g2-after", "g1-after",
	}, order)
}

func TestMiddleware_ShortCircuit(t *testing.T) {
	s := newTestServer()

	handlerCalled := false
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/blocked", Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		}},
	}, WithMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Do not call next; return directly
			WriteHTTPError(w, http.StatusForbidden, "blocked")
		}
	}))

	rec := doRequest(t, s, http.MethodGet, "/blocked", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, handlerCalled, "handler should not be called when middleware short-circuits")
}

// TestMiddleware_UseAfterAddRoute verifies that global middleware added via Use also
// affects routes that are "already registered".
// This is the test for the core bug fix: AddRoutes used to bake global middleware into
// the handler at registration time, so middleware added later via Use could not affect
// already-registered routes.
func TestMiddleware_UseAfterAddRoute(t *testing.T) {
	var order []string
	s := newTestServer()

	// Register the route first
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		},
	})

	// Then add global middleware (the docs promise it applies to registered routes)
	s.Use(recordingMiddleware("global", &order))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// The global middleware should apply to the already-registered route
	assert.Equal(t, []string{"global-before", "handler", "global-after"}, order)
}

// TestMiddleware_UseAfterAddRouteMultiple verifies that several global middleware added
// after route registration also take effect in order.
func TestMiddleware_UseAfterAddRouteMultiple(t *testing.T) {
	var order []string
	s := newTestServer()

	// Register the route first
	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		},
	})

	// Then add several global middleware
	s.Use(
		recordingMiddleware("g1", &order),
		recordingMiddleware("g2", &order),
	)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// g1 → g2 → handler
	assert.Equal(t, []string{"g1-before", "g2-before", "handler", "g2-after", "g1-after"}, order)
}

// TestMiddleware_UseBeforeAndAfterAddRoute verifies that when Use is called both before
// and after route registration, the global and group middleware both apply correctly to
// the routes with the right execution order.
func TestMiddleware_UseBeforeAndAfterAddRoute(t *testing.T) {
	var order []string
	s := newTestServer()

	// First add one global middleware
	s.Use(recordingMiddleware("g1", &order))

	// Register the route (with group middleware)
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	}, WithMiddleware(recordingMiddleware("grp", &order)))

	// Then add another global middleware
	s.Use(recordingMiddleware("g2", &order))

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// g1 → g2 → grp → handler
	assert.Equal(t, []string{
		"g1-before", "g2-before",
		"grp-before",
		"handler",
		"grp-after",
		"g2-after", "g1-after",
	}, order)
}

// TestMiddleware_GlobalOnNotFound verifies that global middleware also applies to 404
// requests.
// Because global middleware wraps the whole mux, the 404 handler passes through it too.
func TestMiddleware_GlobalOnNotFound(t *testing.T) {
	var order []string
	s := newTestServer()

	s.Use(recordingMiddleware("global", &order))
	// No routes registered, so the request 404s

	rec := doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Both the before and after halves of the global middleware should run
	assert.Equal(t, []string{"global-before", "global-after"}, order)
}

// TestMiddleware_GlobalShortCircuit verifies that when global middleware short-circuits,
// the route handler is never reached.
func TestMiddleware_GlobalShortCircuit(t *testing.T) {
	handlerCalled := false
	s := newTestServer()

	s.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			OkJSON(w, "ok")
		},
	})

	// Global middleware short-circuits: next is never called
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			WriteHTTPError(w, http.StatusUnauthorized, "unauthorized")
		}
	})

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, handlerCalled, "handler should not be called when global middleware short-circuits")
}

// --- ApplyMiddleware standalone function tests ---

func TestApplyMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()

	wrapped := ApplyMiddleware(recordingMiddleware("mw", &order),
		Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)
	s.AddRoutes(wrapped)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw-before", "handler", "mw-after"}, order)
}

func TestApplyMiddlewares(t *testing.T) {
	var order []string
	s := newTestServer()

	mws := []Middleware{
		recordingMiddleware("mw1", &order),
		recordingMiddleware("mw2", &order),
	}
	wrapped := ApplyMiddlewares(mws,
		Route{Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)
	s.AddRoutes(wrapped)

	rec := doRequest(t, s, http.MethodGet, "/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

// --- Group tests ---

func TestGroup(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api")
	api.AddRoute(Route{
		Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") },
	})

	rec := doRequest(t, s, http.MethodGet, "/api/ping", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "pong")
}

func TestGroup_WithMiddleware(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api", recordingMiddleware("mw1", &order))
	api.Use(recordingMiddleware("mw2", &order))
	api.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "ok")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/api/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

func TestGroup_Nested(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api", recordingMiddleware("api-mw", &order))
	v1 := api.Group("/v1", recordingMiddleware("v1-mw", &order))
	v1.AddRoute(Route{
		Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
			OkJSON(w, "users")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "users")

	// api-mw → v1-mw → handler
	assert.Equal(t, []string{"api-mw-before", "v1-mw-before", "handler", "v1-mw-after", "api-mw-after"}, order)
}

func TestGroup_AddRoutes(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api/v1")
	api.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "list") }},
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, r.PathValue("id")) }},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "list")

	rec2 := doRequest(t, s, http.MethodGet, "/api/v1/users/99", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "99")
}

// TestGroup_AddRouteWithOptions verifies Group.AddRoute now accepts RouteOption too,
// keeping its API consistent with Server.AddRoute.
func TestGroup_AddRouteWithOptions(t *testing.T) {
	s := newTestServer()

	api := s.Group("/api/v1")
	// Group.AddRoute carries additional middleware (RouteOption)
	api.AddRoute(Route{
		Method: "GET", Path: "/test", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	}, WithMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-MW", "applied")
			next(w, r)
		}
	}))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/test", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
	assert.Equal(t, "applied", rec.Header().Get("X-MW"))
}

// TestGroup_AddRoutesWithOptions verifies Group.AddRoutes accepts RouteOption as well.
func TestGroup_AddRoutesWithOptions(t *testing.T) {
	var order []string
	s := newTestServer()

	api := s.Group("/api/v1")
	api.AddRoutes([]Route{
		{Method: "GET", Path: "/a", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler-a")
		}},
		{Method: "GET", Path: "/b", Handler: func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler-b")
		}},
	}, WithMiddleware(recordingMiddleware("extra", &order)))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/a", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"extra-before", "handler-a", "extra-after"}, order)
}

// --- Path parameter tests ---

func TestWildcardPath(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method: "GET",
		Path:   "/files/{path...}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			p := r.PathValue("path")
			OkJSON(w, p)
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/files/dir/sub/file.txt", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "dir/sub/file.txt")
}

// --- Routes / PrintRoutes tests ---

func TestRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
	}, WithPrefix("/api"))

	routes := s.Routes()
	assert.Len(t, routes, 2)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/api/users", routes[0].Path)
	assert.Equal(t, "POST", routes[1].Method)
	assert.Equal(t, "/api/users", routes[1].Path)

	// Verify a copy is returned
	routes[0].Method = "DELETE"
	original := s.Routes()
	assert.Equal(t, "GET", original[0].Method)
}

func TestPrintRoutes(t *testing.T) {
	s := newTestServer()
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {}},
	}, WithPrefix("/api"))

	// It only needs to not panic
	s.PrintRoutes()
}

func TestPrintRoutes_Empty(t *testing.T) {
	s := newTestServer()
	// It only needs to not panic
	s.PrintRoutes()
}

// --- Helper function tests ---

func TestBuildPattern(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/users", "GET /users"},
		{"get", "/users", "GET /users"},
		{"POST", "/users/create", "POST /users/create"},
		{"", "/health", "/health"},
		{"*", "/health", "/health"},
		// A path without a leading slash should be completed automatically
		{"GET", "users", "GET /users"},
		{"POST", "users/create", "POST /users/create"},
		{"", "health", "/health"},
		// An empty path is treated as the root path
		{"GET", "", "GET /"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, buildPattern(tt.method, tt.path))
		})
	}
}

func TestAddRoute_WithoutLeadingSlash(t *testing.T) {
	// When a route Path lacks a leading slash it should be completed automatically
	// rather than panicking
	s := newTestServer()
	s.AddRoute(Route{Method: "GET", Path: "users", Handler: func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, "ok")
	}})
	s.AddRoute(Route{Method: "POST", Path: "users", Handler: func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, "ok")
	}})

	rec := doRequest(t, s, http.MethodGet, "/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = doRequest(t, s, http.MethodPost, "/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		prefix string
		path   string
		want   string
	}{
		{"", "/users", "/users"},
		{"/api", "/users", "/api/users"},
		{"/api/", "/users", "/api/users"},
		{"/api", "users", "/api/users"},
		{"/api/v1", "/users/{id}", "/api/v1/users/{id}"},
		{"/api", "/", "/api"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix+"+"+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, joinPath(tt.prefix, tt.path))
		})
	}
}

// --- Server configuration tests ---

func TestNewServer_DefaultHost(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080})
	assert.Equal(t, "0.0.0.0", s.conf.Host)
	assert.Equal(t, 8080, s.conf.Port)
}

func TestNewServer_CustomHost(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 9090})
	assert.Equal(t, "127.0.0.1", s.conf.Host)
	assert.Equal(t, 9090, s.conf.Port)
}

func TestNewServer_DefaultTimeouts(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080})
	assert.Equal(t, 10*time.Second, s.conf.ReadTimeout)
	assert.Equal(t, 10*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 1048576, s.conf.MaxHeaderBytes)
	assert.Equal(t, 10*time.Second, s.conf.ShutdownTimeout)
}

func TestNewServer_WithOptions(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080},
		WithReadTimeout(30*time.Second),
		WithWriteTimeout(60*time.Second),
		WithIdleTimeout(120*time.Second),
		WithMaxHeaderBytes(1<<20),
		WithShutdownTimeout(5*time.Second),
	)
	assert.Equal(t, 30*time.Second, s.conf.ReadTimeout)
	assert.Equal(t, 60*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 1<<20, s.conf.MaxHeaderBytes)
	assert.Equal(t, 5*time.Second, s.conf.ShutdownTimeout)
}

// TestNewServer_ZeroTimeoutViaRunOption verifies a timeout can be explicitly set to 0
// (no limit) via RunOption and is not overridden by fillDefault's default value.
func TestNewServer_ZeroTimeoutViaRunOption(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080},
		WithReadTimeout(0),
		WithWriteTimeout(0),
		WithIdleTimeout(0),
		WithShutdownTimeout(0),
	)
	assert.Equal(t, time.Duration(0), s.conf.ReadTimeout, "WithReadTimeout(0) must win over the default")
	assert.Equal(t, time.Duration(0), s.conf.WriteTimeout)
	assert.Equal(t, time.Duration(0), s.conf.IdleTimeout)
	assert.Equal(t, time.Duration(0), s.conf.ShutdownTimeout)
}

// TestNewServer_ZeroTimeoutViaConfig verifies that setting 0 via ServerConfig is treated
// as "unset" and the default value applies.
func TestNewServer_ZeroTimeoutViaConfig(t *testing.T) {
	s := NewServer(ServerConfig{Port: 8080}) // all timeouts are zero values
	assert.Equal(t, 10*time.Second, s.conf.ReadTimeout, "zero value in ServerConfig should use the default")
	assert.Equal(t, 10*time.Second, s.conf.WriteTimeout)
	assert.Equal(t, 120*time.Second, s.conf.IdleTimeout)
	assert.Equal(t, 10*time.Second, s.conf.ShutdownTimeout)
}

// --- Integration tests ---

func TestServer_Integration(t *testing.T) {
	var mu sync.Mutex
	requests := make(map[string]string)

	s := newTestServer()

	// Global logging middleware
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r)
		}
	})

	// Public route
	s.AddRoute(Route{
		Method: "GET", Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "ok") },
	})

	// API v1 route group (with prefix and middleware)
	s.AddRoutes([]Route{
		{Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests["list"] = "called"
			mu.Unlock()
			OkJSON(w, "list")
		}},
		{Method: "GET", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, r.PathValue("id"))
		}},
		{Method: "POST", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "created")
		}},
	}, WithPrefix("/api/v1"))

	// Create a child route group with Group
	admin := s.Group("/admin")
	admin.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				WriteHTTPError(w, http.StatusForbidden, "admin required")
				return
			}
			next(w, r)
		}
	})
	admin.AddRoute(Route{
		Method: "DELETE", Path: "/users/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "deleted")
		}},
	)

	// Test the public route
	rec := doRequest(t, s, http.MethodGet, "/health", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")

	// Test the API v1 routes
	rec = doRequest(t, s, http.MethodGet, "/api/v1/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "list")

	// Test path parameters
	rec = doRequest(t, s, http.MethodGet, "/api/v1/users/42", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "42")

	// Test POST
	rec = doRequest(t, s, http.MethodPost, "/api/v1/users", strings.NewReader(`{"name":"test"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "created")

	// Test the admin route (no header, so it should be blocked)
	rec = doRequest(t, s, http.MethodDelete, "/admin/users/1", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Test the admin route (with header, so it should pass)
	rec = doRequestWithHeaders(t, s, http.MethodDelete, "/admin/users/1", nil, map[string]string{"X-Admin": "true"})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "deleted")

	// Print the routes
	s.PrintRoutes()

	// Verify the route count
	routes := s.Routes()
	assert.Len(t, routes, 5)
}

// --- Start/Shutdown tests ---

func TestServer_StartAndShutdown(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0, ShutdownTimeout: 2 * time.Second})
	s.AddRoute(Route{
		Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") },
	})

	// Find a free port
	ln, err := newTestListener()
	require.NoError(t, err)
	port := ln.Addr().(*testAddr).port
	ln.Close()

	s.conf.Port = port

	// Start the server
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	// Wait for the server to be ready
	var lastErr error
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", port))
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		lastErr = nil
		break
	}
	require.NoError(t, lastErr, "server should be ready")

	// Shut down the server
	err = s.Shutdown()
	require.NoError(t, err)

	// Verify the server is closed
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

func TestServer_ShutdownNotStarted(t *testing.T) {
	s := newTestServer()
	err := s.Shutdown()
	assert.NoError(t, err)
}

func TestServer_Stop(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0, ShutdownTimeout: 2 * time.Second})
	s.AddRoute(Route{
		Method: "GET", Path: "/ping", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "pong") },
	})

	ln, err := newTestListener()
	require.NoError(t, err)
	port := ln.Addr().(*testAddr).port
	ln.Close()
	s.conf.Port = port

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	// Wait for the server to be ready
	var lastErr error
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", port))
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		lastErr = nil
		break
	}
	require.NoError(t, lastErr, "server should be ready")

	// Stop returns an error and delegates to Shutdown
	assert.NoError(t, s.Stop())

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

func TestServer_StopNotStarted(t *testing.T) {
	s := newTestServer()
	assert.NoError(t, s.Stop())
}

func TestServer_ContextPropagation(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{
		Method: "GET", Path: "/ctx", Handler: func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			_ = ctx // the context should be usable for cancellation, timeouts, etc.
			OkJSON(w, "ok")
		}},
	)

	rec := doRequest(t, s, http.MethodGet, "/ctx", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- net.Listener helpers ---

// testAddr is used to obtain a test port.
type testAddr struct {
	network string
	port    int
}

func (a *testAddr) Network() string { return a.network }
func (a *testAddr) String() string  { return fmt.Sprintf("127.0.0.1:%d", a.port) }

// testListener is used to obtain an available port.
type testListener struct {
	addr *testAddr
}

func (l *testListener) Accept() (net.Conn, error) { return nil, nil }
func (l *testListener) Close() error              { return nil }
func (l *testListener) Addr() net.Addr            { return l.addr }

func newTestListener() (*testListener, error) {
	// Use net.Listen to grab an available port, then close it
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close()
	return &testListener{addr: &testAddr{network: "tcp", port: addr.Port}}, nil
}

// --- Custom error response tests ---

func TestSetNotFoundHandler(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		WriteHTTPError(w, http.StatusNotFound, "custom not found")
	})

	rec := doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp Response[any]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "custom not found", resp.Msg)
	assert.NotContains(t, rec.Body.String(), "404 page not found")
}

func TestSetNotFoundHandler_ExistingRoute(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, NewCodeError(CodeNotFound, "custom not found"))
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	rec := doRequest(t, s, http.MethodGet, "/users", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "users")

	rec = doRequest(t, s, http.MethodGet, "/nonexistent", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "custom not found")
}

func TestSetNotFoundHandler_MethodNotAllowed(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, NewCodeError(CodeNotFound, "custom not found"))
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) { OkJSON(w, "users") },
	})

	// A 405 should not be taken over by the 404 handler
	rec := doRequest(t, s, http.MethodPost, "/users", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestSetNotFoundHandler_Business404NotHijacked verifies a 404 deliberately returned by
// a business route is not hijacked by the global 404 handler (historical defect:
// intercepting the 404 written to the ResponseWriter replaced business 404s and
// swallowed the business response body).
func TestSetNotFoundHandler_Business404NotHijacked(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, NewCodeError(CodeNotFound, "custom not found"))
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/users/{id}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			// A business-level "resource not found" must reach the client unchanged
			WriteHTTPError(w, http.StatusNotFound, "user not found")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/users/42", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp Response[any]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "user not found", resp.Msg)
	assert.NotContains(t, rec.Body.String(), "custom not found")
}

// TestSetNotFoundHandler_BusinessOtherStatusesNotHijacked verifies other status codes are
// equally unaffected.
func TestSetNotFoundHandler_BusinessOtherStatusesNotHijacked(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		OkJSON(w, NewCodeError(CodeNotFound, "custom not found"))
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/teapot",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("teapot body"))
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/teapot", nil)
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "teapot body", rec.Body.String())
}

// capsWriter implements Flusher / Hijacker / Pusher / Unwrap,
// used to verify optional interfaces are not swallowed by the ResponseWriter wrapper.
type capsWriter struct {
	*httptest.ResponseRecorder
}

func (w *capsWriter) Flush() { w.ResponseRecorder.Flush() }

func (w *capsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("test: hijack not supported")
}

func (w *capsWriter) Push(string, *http.PushOptions) error { return nil }

func (w *capsWriter) Unwrap() http.ResponseWriter { return w.ResponseRecorder }

// TestSetNotFoundHandler_PreservesWriterCapabilities verifies that after setting a custom
// 404, handlers of normal routes can still see Flush / Hijack / Push / Unwrap
// (historical defect: the wrapping writer did not implement these interfaces, silently
// breaking SSE / WebSocket / HTTP2).
func TestSetNotFoundHandler_PreservesWriterCapabilities(t *testing.T) {
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		WriteHTTPError(w, http.StatusNotFound, "custom not found")
	})

	var hasFlusher, hasHijacker, hasPusher, hasUnwrap bool
	var flushed bool
	s.AddRoute(Route{
		Method: "GET", Path: "/stream",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			_, hasFlusher = w.(http.Flusher)
			_, hasHijacker = w.(http.Hijacker)
			_, hasPusher = w.(http.Pusher)
			_, hasUnwrap = w.(interface{ Unwrap() http.ResponseWriter })

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("chunk"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
				flushed = true
			}
		},
	})

	rec := httptest.NewRecorder()
	cw := &capsWriter{ResponseRecorder: rec}
	s.Handler().ServeHTTP(cw, httptest.NewRequest(http.MethodGet, "/stream", nil))

	assert.True(t, hasFlusher, "http.Flusher must not be swallowed")
	assert.True(t, hasHijacker, "http.Hijacker must not be swallowed")
	assert.True(t, hasPusher, "http.Pusher must not be swallowed")
	assert.True(t, hasUnwrap, "Unwrap (http.ResponseController) must not be swallowed")
	assert.True(t, flushed)
	assert.True(t, rec.Flushed, "Flush should reach the underlying writer")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "chunk", rec.Body.String())
}

// TestSetNotFoundHandler_CustomHandlerReceivesCapableWriter verifies the custom 404
// handler itself also receives the full writer capabilities.
func TestSetNotFoundHandler_CustomHandlerReceivesCapableWriter(t *testing.T) {
	var hasFlusher, hasHijacker, hasPusher, hasUnwrap bool
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		_, hasFlusher = w.(http.Flusher)
		_, hasHijacker = w.(http.Hijacker)
		_, hasPusher = w.(http.Pusher)
		_, hasUnwrap = w.(interface{ Unwrap() http.ResponseWriter })
		w.WriteHeader(http.StatusNotFound)
	})

	rec := httptest.NewRecorder()
	cw := &capsWriter{ResponseRecorder: rec}
	s.Handler().ServeHTTP(cw, httptest.NewRequest(http.MethodGet, "/missing", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.True(t, hasFlusher)
	assert.True(t, hasHijacker)
	assert.True(t, hasPusher)
	assert.True(t, hasUnwrap)
}

// TestSetNotFoundHandler_CustomHandlerSeesOriginalRequest verifies the custom 404
// handler still receives the original request (including method / path / query).
func TestSetNotFoundHandler_CustomHandlerSeesOriginalRequest(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	s := newTestServer()
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusNotFound)
	})

	rec := doRequest(t, s, http.MethodPost, "/a/b?x=1", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/a/b", gotPath)
	assert.Equal(t, "x=1", gotQuery)
}
