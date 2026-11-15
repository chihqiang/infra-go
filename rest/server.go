package rest

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/chihqiang/infra-go/rest/chain"
	resthandler "github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/internal/cors"
	"github.com/chihqiang/infra-go/rest/internal/realip"
	"github.com/chihqiang/infra-go/rest/router"
)

// Server is an HTTP server with a method-aware router.
type Server struct {
	router             *router.Router
	httpServer         *http.Server
	certFile           string
	keyFile            string
	shutdownTimeout    time.Duration
	handlerMu          sync.RWMutex
	handler            http.Handler
	frontMiddlewares   []chain.Middleware
	outerMiddlewares   []chain.Middleware
	tailMiddlewares    []chain.Middleware
	cors               *corsSetting
	pendingCorsHeaders []string
	routesMu           sync.RWMutex
	routes             []Route
	addrMu             sync.RWMutex
	addr               string
	tlsConfig          *tls.Config
}

// NewServer creates a server using the given configuration. The zero Config is
// replaced by DefaultConfig; a partially populated Config is used literally.
func NewServer(conf Config, opts ...Option) (*Server, error) {
	conf = conf.withDefaults()
	if err := conf.Validate(); err != nil {
		return nil, err
	}

	routes := router.New()
	shutdownTimeout := conf.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}
	trustedProxies, err := realip.ParseTrustedProxies(conf.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("rest: %w", err)
	}
	front, tail := buildMiddlewares(conf, trustedProxies)
	server := &Server{
		router:           routes,
		certFile:         conf.CertFile,
		keyFile:          conf.KeyFile,
		shutdownTimeout:  shutdownTimeout,
		frontMiddlewares: front,
		tailMiddlewares:  tail,
		addr:             net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port)),
	}
	for _, opt := range opts {
		opt(server)
	}
	server.rebuildHandler()
	server.httpServer = &http.Server{
		Addr:           net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port)),
		Handler:        server,
		ReadTimeout:    conf.ReadTimeout,
		WriteTimeout:   conf.WriteTimeout,
		IdleTimeout:    conf.IdleTimeout,
		MaxHeaderBytes: conf.MaxHeaderBytes,
		TLSConfig:      server.tlsConfig,
	}

	return server, nil
}

// buildMiddlewares assembles the server chain in execution order: the first
// entry wraps all later ones. front holds the observability middleware;
// tail starts with Timeout, so middlewares inserted between them (the breaker)
// observe timeout responses without running before request IDs and logs.
func buildMiddlewares(conf Config, trustedProxies []netip.Prefix) (front, tail []chain.Middleware) {
	if conf.Middlewares.Recovery {
		front = append(front, resthandler.RecoverHandler)
	}
	if conf.Middlewares.RequestID {
		front = append(front, resthandler.RequestIDHandler)
	}
	if conf.Middlewares.Trace {
		front = append(front, resthandler.TraceHandler(
			resthandler.WithTraceServiceName(conf.Name),
			resthandler.WithTraceIgnorePaths(conf.TraceIgnorePaths...),
		))
	}
	if conf.Middlewares.Log {
		options := []resthandler.LogOption{
			resthandler.WithLogServiceName(conf.Name),
			resthandler.WithLogSkipPaths(conf.LogSkipPaths...),
		}
		if len(trustedProxies) > 0 {
			options = append(options, resthandler.WithLogClientIP(func(r *http.Request) string {
				return realip.FromRequestTrusted(r, trustedProxies)
			}))
		}
		front = append(front, resthandler.LogHandler(options...))
	}
	if conf.Middlewares.MaxConns > 0 {
		front = append(front, resthandler.MaxConnsHandler(conf.Middlewares.MaxConns))
	}

	if conf.Middlewares.Timeout > 0 {
		tail = append(tail, resthandler.TimeoutHandler(conf.Middlewares.Timeout))
	}
	// Gunzip must run before MaxBodyBytes: the limit has to apply to the
	// decompressed payload, otherwise a small gzip body can expand without
	// bound (decompression bomb).
	if conf.Middlewares.Gunzip {
		tail = append(tail, resthandler.GunzipHandler)
	}
	if conf.MaxBodyBytes > 0 {
		tail = append(tail, resthandler.MaxBytesHandler(conf.MaxBodyBytes))
	}

	return front, tail
}

// Handle registers a handler for the given method and path pattern.
func (s *Server) Handle(method, routePath string, handler http.Handler) {
	s.register(method, routePath, handler, true)
}

// register adds a route to the mux and optionally records it for Routes and
// PrintRoutes. Aliases such as the exact "/path/{$}" form are registered but not
// recorded.
func (s *Server) register(method, routePath string, handler http.Handler, record bool) {
	s.router.Handle(method, routePath, handler)
	if !record {
		return
	}

	s.routesMu.Lock()
	defer s.routesMu.Unlock()
	s.routes = append(s.routes, Route{Method: method, Path: routePath, Handler: handler})
}

// HandleFunc registers a handler function for the given method and path pattern.
func (s *Server) HandleFunc(method, routePath string, handler http.HandlerFunc) {
	s.Handle(method, routePath, handler)
}

// Router returns the server's router.
func (s *Server) Router() *router.Router {
	return s.router
}

// SetNotFoundHandler sets the handler used when no route matches a request.
func (s *Server) SetNotFoundHandler(handler http.Handler) {
	s.router.SetNotFoundHandler(handler)
}

// SetNotAllowedHandler sets the handler used when a route path matches but the method does not.
func (s *Server) SetNotAllowedHandler(handler http.Handler) {
	s.router.SetNotAllowedHandler(handler)
}

// Use adds middleware to the server's request chain, after the built-ins.
func (s *Server) Use(middleware chain.Middleware) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.tailMiddlewares = append(s.tailMiddlewares, middleware)
	s.rebuildHandler()
}

func (s *Server) rebuildHandler() {
	routes := http.Handler(s.router)
	if s.cors != nil && s.cors.notAllowedFn != nil {
		notAllowed := cors.NotAllowedHandler(s.cors.notAllowedFn, s.cors.origins...)
		router, fallback := s.router, notAllowed
		routes = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if router.IsNotFound(r) {
				fallback.ServeHTTP(w, r)
				return
			}
			router.ServeHTTP(w, r)
		})
	}

	// Outer middleware wraps the built-in chain, so it also observes responses
	// generated by Timeout (for example a 503 that the breaker must count).
	middlewares := make([]chain.Middleware, 0,
		len(s.frontMiddlewares)+len(s.outerMiddlewares)+len(s.tailMiddlewares))
	middlewares = append(middlewares, s.frontMiddlewares...)
	middlewares = append(middlewares, s.outerMiddlewares...)
	middlewares = append(middlewares, s.tailMiddlewares...)
	handler := s.withPattern(chain.New(middlewares...).Then(routes))
	s.handler = handler
	if s.cors != nil {
		s.handler = cors.Middleware(s.cors.headersFn(), s.cors.origins...)(s.handler.ServeHTTP)
	}
}

// withPattern injects the matched route template into the request context so
// middleware sees the pattern instead of the concrete path.
func (s *Server) withPattern(next http.Handler) http.Handler {
	if next == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pattern := s.router.MatchedPattern(r); pattern != "" {
			r = r.WithContext(resthandler.ContextWithPattern(r.Context(), pattern))
		}
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP dispatches a request through the server's router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handlerMu.RLock()
	handler := s.handler
	s.handlerMu.RUnlock()
	handler.ServeHTTP(w, r)
}

// Start serves requests until the server is stopped or encounters an error.
// The bound address is recorded so Addr reports the real port, including when
// Port is 0 and when TLS is enabled.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

// Serve serves requests on the supplied listener and records its address so
// Addr reflects the bound port.
func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("rest: listener must not be nil")
	}

	s.addrMu.Lock()
	s.addr = listener.Addr().String()
	s.addrMu.Unlock()

	if s.certFile != "" && s.keyFile != "" {
		err := s.httpServer.ServeTLS(listener, s.certFile, s.keyFile)
		// ServeTLS returns before taking ownership when the certificate cannot
		// be loaded; close the listener in that case so callers do not leak it.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = listener.Close()
		}
		return err
	}
	return s.httpServer.Serve(listener)
}

// Addr returns the address the server listens on. Before Serve or Start it is
// the configured address.
func (s *Server) Addr() string {
	s.addrMu.RLock()
	defer s.addrMu.RUnlock()
	return s.addr
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Stop gracefully shuts down the server, forcing connections closed on timeout.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return nil
}

// Close immediately closes the server and its active connections.
func (s *Server) Close() error {
	return s.httpServer.Close()
}
