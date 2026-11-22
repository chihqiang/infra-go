package rest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chihqiang/ten-go/core/breaker"
	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/core/service"
	"github.com/chihqiang/ten-go/core/trace"
	resthandler "github.com/chihqiang/ten-go/rest/handler"
)

func TestServerServeAndStop(t *testing.T) {
	server, err := NewServer(Config{ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	if err := server.Serve(nil); err == nil {
		t.Fatal("Serve(nil) should return an error")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	response, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = (%d, %q), want (%d, %q)",
			response.StatusCode, body, http.StatusOK, "ok")
	}

	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
	}
}

func TestServerCustomFallbackHandlers(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/users", func(http.ResponseWriter, *http.Request) {})
	server.SetNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	server.SetNotAllowedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not allowed", http.StatusMethodNotAllowed)
	}))

	tests := []struct {
		method string
		path   string
		status int
		body   string
	}{
		{method: http.MethodGet, path: "/missing", status: http.StatusNotFound, body: "not found\n"},
		{method: http.MethodPost, path: "/users", status: http.StatusMethodNotAllowed, body: "not allowed\n"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != test.status || recorder.Body.String() != test.body {
			t.Errorf("%s %s: response = %d %q, want %d %q",
				test.method, test.path, recorder.Code, recorder.Body.String(), test.status, test.body)
		}
	}
}

func TestServerAddrReflectsBoundListener(t *testing.T) {
	server, err := NewServer(Config{ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	initial := server.Addr()
	deadline := time.Now().Add(time.Second)
	for server.Addr() == initial && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := server.Addr(); got != listener.Addr().String() {
		t.Fatalf("Addr() = %q, want %q", got, listener.Addr().String())
	}

	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
	}
}

func TestServerUseIsSafeDuringRequests(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 25; j++ {
				recorder := httptest.NewRecorder()
				server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
				if recorder.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", recorder.Code)
					return
				}
			}
		}()
	}

	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { next(w, r) }
	})
	group.Wait()
}

func TestServerRecoveryMiddlewareFromConfig(t *testing.T) {
	server, err := NewServer(Config{Middlewares: MiddlewaresConf{Recovery: resthandler.RecoveryConfig{Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/panic", func(http.ResponseWriter, *http.Request) {
		panic("handler failure")
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestServerUseAppliesMiddleware(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Middleware", "applied")
			next(w, r)
		}
	})
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := recorder.Header().Get("X-Test-Middleware"); got != "applied" {
		t.Fatalf("middleware header = %q, want applied", got)
	}
}

func TestServerRequestIDMiddlewareFromConfig(t *testing.T) {
	server, err := NewServer(Config{Middlewares: MiddlewaresConf{RequestID: resthandler.RequestIDConfig{Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(w http.ResponseWriter, r *http.Request) {
		id, ok := resthandler.RequestIDFromContext(r.Context())
		if ok {
			w.Header().Set("X-Observed-Request-ID", id)
		}
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	requestID := recorder.Header().Get(resthandler.RequestIDHeader)
	if requestID == "" {
		t.Fatal("request ID response header is empty")
	}
	if got := recorder.Header().Get("X-Observed-Request-ID"); got != requestID {
		t.Fatalf("context request ID = %q, response request ID = %q", got, requestID)
	}
}

func TestServerMaxBodyBytesFromConfig(t *testing.T) {
	server, err := NewServer(Config{
		Middlewares: MiddlewaresConf{
			RequestID: resthandler.RequestIDConfig{Enabled: true},
			MaxBytes:  resthandler.MaxBytesConfig{Enabled: true, Limit: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	server.HandleFunc(http.MethodPost, "/upload", func(http.ResponseWriter, *http.Request) {
		called = true
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("12345"))
	server.ServeHTTP(recorder, request)

	if called {
		t.Fatal("handler was called for an oversized body")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	if recorder.Header().Get(resthandler.RequestIDHeader) == "" {
		t.Fatal("request ID is missing from oversized response")
	}
}

func TestServerMaxBodyBytesChunkedRequest(t *testing.T) {
	server, err := NewServer(Config{Middlewares: MiddlewaresConf{MaxBytes: resthandler.MaxBytesConfig{Enabled: true, Limit: 16}}})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodPost, "/json", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Name string `json:"name"`
		}
		if err := Bind(r, &payload); err != nil {
			_ = ErrorJSON(w, err)
			return
		}
		_ = OkJSON(w, payload)
	})

	// A chunked request has no Content-Length, so the size can only be
	// enforced while reading; that must still surface as 413, not 400.
	body := `{"name":"` + strings.Repeat("a", 1024) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/json", io.NopCloser(strings.NewReader(body)))
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestServerMaxBodyBytesAppliesAfterGunzip(t *testing.T) {
	server, err := NewServer(Config{
		Middlewares: MiddlewaresConf{
			Gunzip:   resthandler.GunzipConfig{Enabled: true},
			MaxBytes: resthandler.MaxBytesConfig{Enabled: true, Limit: 2048},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodPost, "/json", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Name string `json:"name"`
		}
		if err := Bind(r, &payload); err != nil {
			_ = ErrorJSON(w, err)
			return
		}
		_ = OkJSON(w, payload)
	})

	payload := `{"name":"` + strings.Repeat("a", 1024*1024) + `"}`
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed.Len() >= 2048 {
		t.Fatalf("test payload did not compress below the limit: %d bytes", compressed.Len())
	}

	request := httptest.NewRequest(http.MethodPost, "/json", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (decompressed body exceeds the limit)",
			recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestServerTimeoutFromConfig(t *testing.T) {
	server, err := NewServer(Config{Middlewares: MiddlewaresConf{Timeout: resthandler.TimeoutConfig{Enabled: true, Timeout: 10 * time.Millisecond}}})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/slow", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestServerGunzipFromConfig(t *testing.T) {
	server, err := NewServer(Config{Middlewares: MiddlewaresConf{Gunzip: resthandler.GunzipConfig{Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodPost, "/upload", func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write(body)
	})

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/upload", &compressed)
	request.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "payload" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerInjectsRoutePattern(t *testing.T) {
	var pattern string
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			pattern = resthandler.PatternFromContext(r.Context())
			next(w, r)
		}
	})
	server.HandleFunc(http.MethodGet, "/users/{id}", func(http.ResponseWriter, *http.Request) {})

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if pattern != "GET /users/{id}" {
		t.Fatalf("pattern = %q, want GET /users/{id}", pattern)
	}
}

func TestServerStartBindsAndServes(t *testing.T) {
	server, err := NewServer(Config{Host: "127.0.0.1", Port: 0, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})

	started := make(chan error, 1)
	go func() { started <- server.Start() }()

	initial := server.Addr()
	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == initial && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	addr := server.Addr()
	if addr == initial {
		t.Fatalf("Addr() did not update after Start(): %q", addr)
	}

	response, err := http.Get("http://" + addr + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "pong" {
		t.Fatalf("body = %q, want pong", body)
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-started; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Start() error = %v, want http.ErrServerClosed", err)
	}
}

func TestServerRouterAndAddr(t *testing.T) {
	server, err := NewServer(Config{Host: "127.0.0.1", Port: 9090})
	if err != nil {
		t.Fatal(err)
	}
	if server.Router() == nil {
		t.Fatal("Router() returned nil")
	}
	if got := server.Addr(); got != "127.0.0.1:9090" {
		t.Fatalf("Addr() = %q, want 127.0.0.1:9090", got)
	}
}

func TestServerAddRoutes(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.AddRoutes(
		Route{Method: http.MethodGet, Path: "/a", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		Route{Method: http.MethodPost, Path: "/b", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
	)

	if len(server.Routes()) != 2 {
		t.Fatalf("routes = %d, want 2", len(server.Routes()))
	}
}

func TestWithTLSConfigStoresConfig(t *testing.T) {
	conf := &tls.Config{MinVersion: tls.VersionTLS12}
	server, err := NewServer(Config{}, WithTLSConfig(conf))
	if err != nil {
		t.Fatal(err)
	}
	if server.httpServer.TLSConfig != conf {
		t.Fatal("TLS config was not applied")
	}
}

func TestWithBreakerWrapsRequests(t *testing.T) {
	server, err := NewServer(Config{}, WithBreaker(breaker.NewBreaker(breaker.WithName("test"))))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestServerDefaultsShutdownTimeout(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if server.shutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("shutdown timeout = %v, want %v", server.shutdownTimeout, defaultShutdownTimeout)
	}
}

func TestServerUseNilMiddlewareIsNoOp(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Use(nil)
	server.HandleFunc(http.MethodGet, "/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

type recordingPromise struct {
	accepted bool
	rejected string
}

func (p *recordingPromise) Accept() { p.accepted = true }

func (p *recordingPromise) Reject(reason string) { p.rejected = reason }

type recordingBreaker struct {
	promise *recordingPromise
}

func (b *recordingBreaker) Name() string { return "recording" }

func (b *recordingBreaker) Allow() (breaker.Promise, error) {
	return b.AllowCtx(context.Background())
}

func (b *recordingBreaker) AllowCtx(context.Context) (breaker.Promise, error) {
	return b.promise, nil
}

func (b *recordingBreaker) Do(func() error) error { return nil }

func (b *recordingBreaker) DoCtx(context.Context, func() error) error { return nil }

func (b *recordingBreaker) DoWithAcceptable(func() error, breaker.Acceptable) error { return nil }

func (b *recordingBreaker) DoWithAcceptableCtx(
	context.Context, func() error, breaker.Acceptable,
) error {
	return nil
}

func (b *recordingBreaker) DoWithFallback(func() error, breaker.Fallback) error { return nil }

func (b *recordingBreaker) DoWithFallbackCtx(
	context.Context, func() error, breaker.Fallback,
) error {
	return nil
}

func (b *recordingBreaker) DoWithFallbackAcceptable(
	func() error, breaker.Fallback, breaker.Acceptable,
) error {
	return nil
}

func (b *recordingBreaker) DoWithFallbackAcceptableCtx(
	context.Context, func() error, breaker.Fallback, breaker.Acceptable,
) error {
	return nil
}

func TestWithBreakerCountsTimeoutResponses(t *testing.T) {
	br := &recordingBreaker{promise: &recordingPromise{}}
	server, err := NewServer(Config{
		Middlewares: MiddlewaresConf{Timeout: resthandler.TimeoutConfig{Enabled: true, Timeout: 5 * time.Millisecond}},
	}, WithBreaker(br))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/slow", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if br.promise.accepted {
		t.Fatal("timeout response must not be accepted by the breaker")
	}
	if br.promise.rejected != "http status 503" {
		t.Fatalf("rejection = %q, want http status 503", br.promise.rejected)
	}
}

func TestNewServerUsesDefaultConfigForZeroValue(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := server.httpServer.Addr; got != "0.0.0.0:8080" {
		t.Fatalf("addr = %q, want 0.0.0.0:8080", got)
	}
	if server.httpServer.ReadTimeout != 30*time.Second || server.httpServer.MaxHeaderBytes != 5<<20 {
		t.Fatalf("timeouts/limits not defaulted: %+v", server.httpServer)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if got := recorder.Header().Get(resthandler.RequestIDHeader); got == "" {
		t.Fatal("default RequestID middleware was not applied")
	}
}

func TestNewServerLeavesPartialConfigLiteral(t *testing.T) {
	server, err := NewServer(Config{Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got := server.httpServer.Addr; got != "127.0.0.1:0" {
		t.Fatalf("addr = %q, want 127.0.0.1:0", got)
	}
	if server.httpServer.ReadTimeout != 0 {
		t.Fatalf("read timeout = %v, want 0 for a literal partial config", server.httpServer.ReadTimeout)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if got := recorder.Header().Get(resthandler.RequestIDHeader); got != "" {
		t.Fatalf("request ID header = %q, want no default middleware", got)
	}
}

func TestWithBreakerObservesSuccessfulResponse(t *testing.T) {
	promise := &recordingPromise{}
	server, err := NewServer(Config{}, WithBreaker(&recordingBreaker{promise: promise}))
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if !promise.accepted {
		t.Fatal("breaker option did not accept a successful response")
	}
}

func TestServerRegisterAddsTraceAgentToServiceGroup(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	server, err := NewServer(Config{
		Host: "127.0.0.1",
		Port: 0,
		Middlewares: MiddlewaresConf{
			Trace: resthandler.TraceConfig{
				Enabled: true,
				Config: trace.Config{
					Name:     "test-service",
					Batcher:  trace.BatcherFile,
					Endpoint: filepath.Join(t.TempDir(), "traces.json"),
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	group := service.NewServiceGroup()
	server.Register(group)

	done := make(chan struct{})
	go func() {
		group.Start()
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !trace.IsAgentRunning() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !trace.IsAgentRunning() {
		t.Fatal("ServiceGroup did not start the trace agent")
	}

	group.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServiceGroup did not stop")
	}
	if trace.IsAgentRunning() {
		t.Fatal("ServiceGroup did not stop the trace agent it started")
	}
}

type lifecycleService struct {
	started chan struct{}
	stopped chan struct{}
}

func newLifecycleService() *lifecycleService {
	return &lifecycleService{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *lifecycleService) Start() { close(s.started) }
func (s *lifecycleService) Stop()  { close(s.stopped) }

func TestServerAddServiceIsRegisteredWithGroup(t *testing.T) {
	server, err := NewServer(Config{Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	extra := newLifecycleService()
	server.AddService(extra)
	server.AddService(nil) // no-op

	group := service.NewServiceGroup()
	server.Register(group)

	done := make(chan struct{})
	go func() {
		group.Start()
		close(done)
	}()

	select {
	case <-extra.started:
	case <-time.After(2 * time.Second):
		t.Fatal("registered auxiliary service was not started")
	}

	group.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServiceGroup did not stop")
	}
	select {
	case <-extra.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("registered auxiliary service was not stopped")
	}
}

func TestNewServerConfiguresGlobalLogger(t *testing.T) {
	previous := logger.GetGlobal()
	t.Cleanup(func() { logger.SetGlobal(previous) })

	server, err := NewServer(Config{Logger: logger.Config{
		ServiceName: "rest-test",
		Mode:        "console",
		Encoding:    "json",
		Level:       "info",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := logger.GetGlobal()
	if got == previous {
		t.Fatal("NewServer did not replace the global logger")
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logger.GetGlobal() != got {
		t.Fatal("Shutdown must not replace the global logger")
	}
	if concrete, ok := got.(*logger.Logger); ok {
		_ = concrete.Close()
	}
}

func TestNewServerLeavesGlobalLoggerWhenUnconfigured(t *testing.T) {
	previous := logger.GetGlobal()
	t.Cleanup(func() { logger.SetGlobal(previous) })

	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if logger.GetGlobal() != previous {
		t.Fatal("zero Logger config must not replace the global logger")
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
