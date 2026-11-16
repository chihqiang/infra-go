package rest

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/core/logger"
	resthandler "github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/internal/realip"
)

var (
	ErrInvalidPort        = errors.New("rest: port must be between 0 and 65535")
	ErrIncompleteTLSFiles = errors.New("rest: cert file and key file must be configured together")
	ErrInvalidHealthPath  = errors.New("rest: invalid health path")
)

// defaultShutdownTimeout is used when Config.ShutdownTimeout is zero, so Stop
// remains graceful instead of forcing connections closed immediately.
const defaultShutdownTimeout = 10 * time.Second

// Config configures an HTTP server.
type Config struct {
	Host            string        `json:",default=0.0.0.0"`
	Port            int           `json:",default=8080,range=[0:65535]"`
	CertFile        string        `json:",optional"`
	KeyFile         string        `json:",optional"`
	ReadTimeout     time.Duration `json:",default=10s"`
	WriteTimeout    time.Duration `json:",default=10s"`
	IdleTimeout     time.Duration `json:",default=120s"`
	ShutdownTimeout time.Duration `json:",default=10s"`
	MaxHeaderBytes  int           `json:",default=1048576"`
	// Logger configures the server-owned logger when any field is set. NewServer
	// builds it, installs it on the server (used by the access-log middleware)
	// and also sets it as the global logger so package-level logger calls in
	// other middleware see the same instance. Use WithLogger to inject a
	// pre-built logger instead.
	Logger      logger.Config
	Middlewares MiddlewaresConf
	// Health configures the built-in GET health endpoint. It is disabled by
	// default so applications that already register their own health route are
	// unaffected.
	Health HealthConfig
}

// defaultHealthPath is used when HealthConfig.Path is empty.
const defaultHealthPath = "/health"

// normalizeHealthPath trims the configured path and makes it absolute, so a
// config value such as "healthz" still registers "/healthz".
func normalizeHealthPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultHealthPath
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// validHealthPath rejects paths that would make the health route surprising:
//   - "/" (and a trailing slash) makes the ServeMux pattern a subtree that matches
//     every request and shadows the 404 handler;
//   - whitespace, query, fragment and wildcard characters are not a static path.
func validHealthPath(path string) bool {
	if path == "" || path == "/" || strings.HasSuffix(path, "/") {
		return false
	}
	return !strings.ContainsAny(path, " \t\r\n?#{}")
}

// HealthConfig configures the built-in health endpoint.
//
// When Enabled is true, NewServer registers GET <Path> (HEAD is handled by the
// same ServeMux pattern) and answers 200 with {"status":"ok"}. The path must be a
// static path: "/" and a trailing slash are rejected because they turn the route
// into a subtree that shadows other handlers.
type HealthConfig struct {
	// Enabled registers the endpoint when the server is created. Default false.
	Enabled bool `json:",optional"`
	// Path is the health endpoint path, default "/health".
	Path string `json:",default=/health"`
}

// MiddlewaresConf groups the configuration of the built-in middleware handlers.
type MiddlewaresConf struct {
	Recovery  resthandler.RecoveryConfig
	RequestID resthandler.RequestIDConfig
	Trace     resthandler.TraceConfig
	Log       resthandler.LogConfig
	MaxConns  resthandler.MaxConnsConfig
	Auth      resthandler.AuthConfig
	RateLimit resthandler.RateLimitConfig
	Breaker   resthandler.BreakerConfig
	Timeout   resthandler.TimeoutConfig
	Gunzip    resthandler.GunzipConfig
	MaxBytes  resthandler.MaxBytesConfig
	Signature resthandler.SignatureConfig
}

// DefaultConfig returns the standard HTTP server configuration.
func DefaultConfig() Config {
	return Config{
		Host:            "0.0.0.0",
		Port:            8080,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: defaultShutdownTimeout,
		MaxHeaderBytes:  1 << 20,
		Health:          HealthConfig{Path: defaultHealthPath},
		Middlewares: MiddlewaresConf{
			Recovery:  resthandler.RecoveryConfig{Enabled: true},
			RequestID: resthandler.RequestIDConfig{Enabled: true},
			Log:       resthandler.LogConfig{Enabled: true},
			MaxBytes:  resthandler.MaxBytesConfig{Enabled: true, Limit: 1 << 20},
		},
	}
}

// withDefaults replaces the zero Config with DefaultConfig. A partially
// populated Config is used as-is, so explicit zero values (Port 0, disabled
// middleware switches) keep their meaning.
func (c Config) withDefaults() Config {
	if reflect.ValueOf(c).IsZero() {
		return DefaultConfig()
	}
	return c
}

// loggerConfigured reports whether the caller configured a logger. The zero
// logger.Config is valid, so only a non-zero value replaces the global logger.
func (c Config) loggerConfigured() bool {
	return !reflect.ValueOf(c.Logger).IsZero()
}

// Validate checks configuration values that cannot be inferred from defaults.
func (c Config) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, c.Port)
	}
	if (c.CertFile == "") != (c.KeyFile == "") {
		return ErrIncompleteTLSFiles
	}
	if c.MaxHeaderBytes < 0 {
		return fmt.Errorf("rest: max header bytes must not be negative: %d", c.MaxHeaderBytes)
	}
	if c.ReadTimeout < 0 || c.WriteTimeout < 0 || c.IdleTimeout < 0 || c.ShutdownTimeout < 0 {
		return errors.New("rest: server timeouts must not be negative")
	}
	if c.Middlewares.MaxConns.Limit < 0 {
		return fmt.Errorf("rest: max connections must not be negative: %d", c.Middlewares.MaxConns.Limit)
	}
	if c.Middlewares.Timeout.Timeout < 0 {
		return errors.New("rest: middleware timeout must not be negative")
	}
	if c.Middlewares.MaxBytes.Limit < 0 {
		return fmt.Errorf("rest: max body bytes must not be negative: %d", c.Middlewares.MaxBytes.Limit)
	}
	if _, err := realip.ParseTrustedProxies(c.Middlewares.Log.TrustedProxies); err != nil {
		return fmt.Errorf("rest: %w", err)
	}
	if c.Middlewares.Auth.Enabled && c.Middlewares.Auth.Parser == nil {
		return errors.New("rest: auth middleware requires a token parser")
	}
	if c.Middlewares.RateLimit.Enabled && c.Middlewares.RateLimit.Limiter == nil {
		return errors.New("rest: rate limit middleware requires a limiter")
	}
	if c.Middlewares.Breaker.Enabled && c.Middlewares.Breaker.Breaker == nil {
		return errors.New("rest: breaker middleware requires a breaker")
	}
	if c.Middlewares.Signature.Enabled && len(c.Middlewares.Signature.Keys) == 0 {
		return errors.New("rest: signature middleware requires at least one key")
	}
	if c.Health.Enabled {
		path := normalizeHealthPath(c.Health.Path)
		if !validHealthPath(path) {
			return fmt.Errorf("%w: %q", ErrInvalidHealthPath, path)
		}
	}
	return nil
}
