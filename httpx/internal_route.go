package httpx

import (
	"net/http"
	"net/http/pprof"
	"strings"
)

// This file provides built-in routes: PprofRoutes builds the standard pprof
// profiling route table.

// --- pprof profiling routes ---

// PprofRoutes returns the standard pprof profiling route list; it is not registered
// on the Server automatically.
//
// prefix specifies the route prefix; when empty it defaults to /debug/pprof.
// The returned routes must be registered manually via Server.AddRoutes and may carry
// RouteOption values (such as middleware).
//
//	// default prefix /debug/pprof
//	server.AddRoutes(httpx.PprofRoutes(""))
//
//	// custom prefix
//	server.AddRoutes(httpx.PprofRoutes("/admin/pprof"))
//
//	// with auth middleware (recommended in production)
//	server.AddRoutes(httpx.PprofRoutes(""), httpx.WithMiddleware(authMiddleware))
//
// The returned route list (11 entries in total):
//
//   - GET {prefix}/              — index page listing all available profiles
//   - GET {prefix}/cmdline       — command line of the current process
//   - GET {prefix}/profile       — CPU profiling (sample duration from the seconds parameter)
//   - GET {prefix}/symbol        — symbol table lookup
//   - GET {prefix}/trace         — execution trace (sample duration from the seconds parameter)
//   - GET {prefix}/allocs        — all memory allocation samples
//   - GET {prefix}/block         — blocking operation stacks (needs runtime.SetBlockProfileRate first)
//   - GET {prefix}/goroutine     — current goroutine stacks
//   - GET {prefix}/heap          — heap memory allocations
//   - GET {prefix}/mutex         — mutex contention (needs runtime.SetMutexProfileFraction first)
//   - GET {prefix}/threadcreate  — OS thread creation
//
// Note: the pprof.Index handler hardcodes the /debug/pprof/ path prefix internally.
// With the default prefix the index page and sub-paths work perfectly; with a custom
// prefix the individual profile endpoints stay reachable, but links inside the index
// page still point at /debug/pprof/ paths.
//
// Security note: pprof endpoints expose internal program information, so production
// deployments should gate access through middleware.
func PprofRoutes(prefix string) []Route {
	// Normalize the prefix: trim leading/trailing /, fall back to the default when
	// empty, and finally make sure it starts with /
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		prefix = "debug/pprof"
	}
	prefix = "/" + prefix
	routes := []Route{
		// Standard handlers
		{Method: http.MethodGet, Path: prefix + "/", Handler: pprof.Index},
		{Method: http.MethodGet, Path: prefix + "/cmdline", Handler: pprof.Cmdline},
		{Method: http.MethodGet, Path: prefix + "/profile", Handler: pprof.Profile},
		{Method: http.MethodGet, Path: prefix + "/symbol", Handler: pprof.Symbol},
		{Method: http.MethodGet, Path: prefix + "/trace", Handler: pprof.Trace},
		// Profile endpoints (pprof.Handler returns an http.Handler; adapt via its
		// ServeHTTP method)
		{Method: http.MethodGet, Path: prefix + "/allocs", Handler: pprof.Handler("allocs").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/block", Handler: pprof.Handler("block").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/goroutine", Handler: pprof.Handler("goroutine").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/heap", Handler: pprof.Handler("heap").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/mutex", Handler: pprof.Handler("mutex").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/threadcreate", Handler: pprof.Handler("threadcreate").ServeHTTP},
	}
	return routes
}
