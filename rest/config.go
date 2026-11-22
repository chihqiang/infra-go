package rest

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/chihqiang/ten-go/core/logger"
	resthandler "github.com/chihqiang/ten-go/rest/handler"
	"github.com/chihqiang/ten-go/rest/internal/healthpath"
)

var (
	ErrInvalidPort        = errors.New("rest: port must be between 0 and 65535")
	ErrIncompleteTLSFiles = errors.New("rest: cert file and key file must be configured together")
	ErrInvalidHealthPath  = errors.New("rest: invalid health path")
)

// defaultShutdownTimeout is used when Config.ShutdownTimeout is zero, so Stop
// remains graceful instead of forcing connections closed immediately.
const defaultShutdownTimeout = 30 * time.Second

// Config configures an HTTP server.
type Config struct {
	Host            string        `json:",default=0.0.0.0"`
	Port            int           `json:",default=8080,range=[0:65535]"`
	CertFile        string        `json:",optional"`
	KeyFile         string        `json:",optional"`
	ReadTimeout     time.Duration `json:",default=30s"`
	WriteTimeout    time.Duration `json:",default=30s"`
	IdleTimeout     time.Duration `json:",default=60s"`
	ShutdownTimeout time.Duration `json:",default=30s"`
	MaxHeaderBytes  int           `json:",default=5242880"`
	// Name names the service in one place. It fills Logger.ServiceName (the "app"
	// field and the volume log directory) and the trace agent and span name
	// (trace.Config.Name) when those sections leave them unset, so each
	// middleware does not have to repeat it.
	Name string `json:",optional"`
	// Logger configures the server-owned logger when any field is set. NewServer
	// builds it, installs it on the server (used by the access-log middleware)
	// and also sets it as the global logger so package-level logger calls in
	// other middleware see the same instance. Use WithLogger to inject a
	// pre-built logger instead.
	Logger      logger.Config   `json:",optional"`
	Middlewares MiddlewaresConf `json:",optional"`
	// Health configures the built-in GET health endpoint. It is disabled by
	// default so applications that already register their own health route are
	// unaffected.
	Health HealthConfig `json:",optional"`
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
	// Path is the health endpoint path; the tag default is used when the health
	// section exists but path is omitted. A config built in code may leave it
	// empty, so NewServer still normalizes it to "/health" when Enabled is true.
	Path string `json:",default=/health,optional"`
}

// MiddlewaresConf groups the configuration of the built-in middleware handlers.
// Keys are the field names and are matched case-insensitively, so `requestID`,
// `maxBytes` and `maxConns` all work. Every entry is optional: an omitted entry
// leaves that middleware at its zero value (disabled). The `default=` tags of a
// handler config apply as soon as the entry itself is present, so `timeout: {}`
// still means "enabled with defaults".
type MiddlewaresConf struct {
	Recovery  resthandler.RecoveryConfig  `json:",optional"`
	RequestID resthandler.RequestIDConfig `json:",optional"`
	Trace     resthandler.TraceConfig     `json:",optional"`
	Log       resthandler.LogConfig       `json:",optional"`
	MaxConns  resthandler.MaxConnsConfig  `json:",optional"`
	Timeout   resthandler.TimeoutConfig   `json:",optional"`
	Gunzip    resthandler.GunzipConfig    `json:",optional"`
	MaxBytes  resthandler.MaxBytesConfig  `json:",optional"`
}

// DefaultConfig returns the standard HTTP server configuration.
func DefaultConfig() Config {
	return Config{
		Host:            "0.0.0.0",
		Port:            8080,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: defaultShutdownTimeout,
		MaxHeaderBytes:  5 << 20,
		Middlewares: MiddlewaresConf{
			Recovery:  resthandler.RecoveryConfig{Enabled: true},
			RequestID: resthandler.RequestIDConfig{Enabled: true},
			Log:       resthandler.LogConfig{Enabled: true},
			MaxBytes:  resthandler.MaxBytesConfig{Enabled: true, Limit: 5 << 20},
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

// WithName propagates Config.Name to the components that carry a service name:
// Logger.ServiceName (the "app" field and the volume log directory) and
// trace.Config.Name. An explicit value in a component section always wins, and
// a section that is not in use is left untouched, so setting Name never enables
// the logger or tracing on its own.
func (c Config) WithName() Config {
	if c.Name == "" {
		return c
	}
	if c.loggerConfigured() && c.Logger.ServiceName == "" {
		c.Logger.ServiceName = c.Name
	}
	if c.Middlewares.Trace.Enabled && c.Middlewares.Trace.Name == "" {
		c.Middlewares.Trace.Name = c.Name
	}
	return c
}

// Setup normalizes and validates the configuration in one step: it applies
// DefaultConfig to a zero Config, propagates Config.Name and validates the
// result. NewServer calls it before building anything; exported so callers
// that inspect or reuse a Config see exactly the values the server will use.
func (c Config) Setup() (Config, error) {
	c = c.withDefaults()
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c.WithName(), nil
}

// Validate checks configuration values that cannot be inferred from defaults.
// Middleware-specific values are only checked when that middleware is enabled,
// so disabled middleware keeps its settings out of validation.
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
	if c.Middlewares.MaxConns.Enabled && c.Middlewares.MaxConns.Limit < 0 {
		return fmt.Errorf("rest: max connections must not be negative: %d", c.Middlewares.MaxConns.Limit)
	}
	if c.Middlewares.Timeout.Enabled && c.Middlewares.Timeout.Timeout < 0 {
		return errors.New("rest: middleware timeout must not be negative")
	}
	if c.Middlewares.MaxBytes.Enabled && c.Middlewares.MaxBytes.Limit < 0 {
		return fmt.Errorf("rest: max body bytes must not be negative: %d", c.Middlewares.MaxBytes.Limit)
	}
	if c.Health.Enabled {
		path := healthpath.Normalize(c.Health.Path)
		if !healthpath.Valid(path) {
			return fmt.Errorf("%w: %q", ErrInvalidHealthPath, path)
		}
	}
	return nil
}
