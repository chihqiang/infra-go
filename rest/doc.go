// Package rest provides a small HTTP (REST) service toolkit: configuration, a
// method-aware router, route groups, a middleware chain, request binding,
// response envelopes and graceful lifecycle management.
//
// Layout:
//
//	rest/            server, routing, config, responses, errors
//	rest/chain       middleware type and immutable chain
//	rest/router      http.ServeMux wrapper with 404/405 detection
//	rest/handler     middleware implementations (recovery, logging, limits, auth, ...)
//	rest/token       JWT issuing and verification
//	rest/internal/   cors, realip, path matching, response writers, binding
//
// Typical usage:
//
//	server, err := rest.NewServer(rest.DefaultConfig())
//	if err != nil {
//		return err
//	}
//
//	server.Group("/api").AddRoute(rest.Route{
//		Method: http.MethodGet,
//		Path:   "/users/{id}",
//		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			_ = rest.OkJSONCtx(r.Context(), w, map[string]string{"id": r.PathValue("id")})
//		}),
//	})
//
//	if err := server.Start(); err != nil {
//		return err
//	}
package rest
