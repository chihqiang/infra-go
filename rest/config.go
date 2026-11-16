package rest

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/chihqiang/infra-go/core/logger"
	resthandler "github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/internal/realip"
)

var (
	ErrInvalidPort        = errors.New("rest: port must be between 0 and 65535")
	ErrIncompleteTLSFiles = errors.New("rest: cert file and key file must be configured together")
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
	return nil
}
