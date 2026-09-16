package httpx

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the concurrency safety of the Server's mutable state (regression):
//   - routes: written by AddRoutes, read by Routes/PrintRoutes
//   - httpServer: written by Start, read by Shutdown/Stop
// Both previously had no lock protection, so -race could detect data races.

func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
}

// TestServer_RoutesConcurrentWithAddRoute is a regression test: concurrent AddRoute and
// Routes must not race.
func TestServer_RoutesConcurrentWithAddRoute(t *testing.T) {
	s := newTestServer()

	const n = 200
	var wg sync.WaitGroup

	// Concurrent writes: register routes (each goroutine uses its own path prefix,
	// because http.ServeMux panics on duplicate patterns)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				s.AddRoute(Route{
					Method:  "GET",
					Path:    fmt.Sprintf("/g%d/r%d", id, j),
					Handler: okHandler(),
				})
			}
		}(i)
	}

	// Concurrent reads: Routes / routesSnapshot
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				_ = s.Routes()
				_ = s.routesSnapshot()
			}
		}()
	}

	wg.Wait()
	assert.Len(t, s.Routes(), 4*n)
}

// TestServer_RoutesSnapshotIsACopy verifies Routes returns a copy, so mutations do not
// affect the internal state.
func TestServer_RoutesSnapshotIsACopy(t *testing.T) {
	s := newTestServer()
	s.AddRoute(Route{Method: "GET", Path: "/a", Handler: okHandler()})

	got := s.Routes()
	require.Len(t, got, 1)

	got[0].Path = "/mutated"
	assert.Equal(t, "/a", s.Routes()[0].Path, "mutating the returned slice must not affect the server")
}

// TestServer_StopConcurrentWithStart is a regression test: Stop/Shutdown running
// concurrently with Start must not race on the httpServer field.
//
// Historical defect: Start wrote s.httpServer without a lock while Shutdown/Stop read
// it without one, detectable by -race (service.ServiceGroup starts and stops in
// different goroutines).
func TestServer_StopConcurrentWithStart(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})
	s.AddRoute(Route{Method: "GET", Path: "/ok", Handler: okHandler()})

	var wg sync.WaitGroup
	// Concurrent Stop/Shutdown calls (reading httpServer)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Stop()
		}()
	}
	// Concurrently simulate the registration done during Start (writing httpServer)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.stateLk.Lock()
			s.httpServer = &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(okHandler())}
			s.stateLk.Unlock()
		}()
	}
	wg.Wait()
}

// TestServer_StopBeforeStartIsNoop verifies that when Start was not called, Stop/Shutdown
// are no-ops and do not panic.
func TestServer_StopBeforeStartIsNoop(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})

	assert.Nil(t, s.currentHTTPServer())
	require.NotPanics(t, func() {
		assert.NoError(t, s.Shutdown())
		assert.NoError(t, s.Stop())
	})
}

// TestServer_ShutdownAfterRegister verifies that after registration, Shutdown really
// acts on that instance.
func TestServer_ShutdownAfterRegister(t *testing.T) {
	s := NewServer(ServerConfig{Host: "127.0.0.1", Port: 0})
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(okHandler())}

	s.stateLk.Lock()
	s.httpServer = srv
	s.stateLk.Unlock()

	require.NotNil(t, s.currentHTTPServer())
	// Calling Shutdown on a never-started http.Server returns either nil or something
	// other than ErrServerClosed; here we only assert it does not panic and is not
	// mistakenly reported as the nil branch
	require.NotPanics(t, func() { _ = s.Shutdown() })
}

// TestServer_ConcurrentRoutesAndHandler verifies route registration and Handler
// construction are concurrency-safe.
func TestServer_ConcurrentRoutesAndHandler(t *testing.T) {
	s := newTestServer()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.AddRoute(Route{
					Method:  "GET",
					Path:    fmt.Sprintf("/h%d/r%d", id, j),
					Handler: okHandler(),
				})
				_ = s.Handler()
			}
		}(i)
	}
	wg.Wait()
	require.NotNil(t, s.Handler())
}

// --- Route template injection (for middleware that aggregates per route) ---

// TestServer_GlobalMiddlewareSeesRoutePattern is a regression test: global middleware
// must be able to read the route template from the context.
//
// Background: global middleware wraps the mux from the outside, and net/http only fills
// r.Pattern when dispatching to the matched handler, so reading r.Pattern inside
// middleware is always empty. Middleware that needs to aggregate per route (circuit
// breaking/metrics) then degrades to aggregating per concrete path, fragmenting the
// statistics and growing memory without bound.
// httpx.Server now pre-checks the route and writes the template into the context.
func TestServer_GlobalMiddlewareSeesRoutePattern(t *testing.T) {
	s := newTestServer()

	var patterns []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			patterns = append(patterns, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/users/{id}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})

	for _, p := range []string{"/users/1", "/users/2", "/users/999"} {
		rec := doRequest(t, s, http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, rec.Code, "path %s", p)
	}

	require.Len(t, patterns, 3)
	for _, got := range patterns {
		assert.Equal(t, "GET /users/{id}", got,
			"global middleware must see the route template, not the concrete path")
	}
}

// TestServer_GlobalMiddlewarePatternEmptyForUnmatched verifies the template is empty
// when no route matches (a 404 request must not be attributed to some route).
func TestServer_GlobalMiddlewarePatternEmptyForUnmatched(t *testing.T) {
	s := newTestServer()

	var seen []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.AddRoute(Route{Method: "GET", Path: "/ok", Handler: okHandler()})

	rec := doRequest(t, s, http.MethodGet, "/missing", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Len(t, seen, 1)
	assert.Empty(t, seen[0], "unmatched requests must not resolve to a route pattern")
}

// TestServer_GlobalMiddlewarePatternWithNotFoundHandler verifies correct behavior when
// a custom 404 handler coexists with template injection.
func TestServer_GlobalMiddlewarePatternWithNotFoundHandler(t *testing.T) {
	s := newTestServer()

	var seen []string
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, middleware.PatternFromContext(r.Context()))
			next(w, r)
		}
	})
	s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
		WriteHTTPError(w, http.StatusNotFound, "custom not found")
	})
	s.AddRoute(Route{Method: "GET", Path: "/users/{id}", Handler: okHandler()})

	// Matched route
	rec := doRequest(t, s, http.MethodGet, "/users/1", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// Unmatched: goes to the custom 404
	rec = doRequest(t, s, http.MethodGet, "/nope", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "custom not found")

	require.Len(t, seen, 2)
	assert.Equal(t, "GET /users/{id}", seen[0])
	assert.Empty(t, seen[1])
}
