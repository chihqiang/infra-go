package rest

import (
	"errors"
	"fmt"
	"time"
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
	// Name identifies the service in trace spans and access logs.
	Name            string        `json:",optional"`
	Host            string        `json:",default=0.0.0.0"`
	Port            int           `json:",default=8080,range=[0:65535]"`
	CertFile        string        `json:",optional"`
	KeyFile         string        `json:",optional"`
	ReadTimeout     time.Duration `json:",default=10s"`
	WriteTimeout    time.Duration `json:",default=10s"`
	IdleTimeout     time.Duration `json:",default=120s"`
	ShutdownTimeout time.Duration `json:",default=10s"`
	MaxHeaderBytes  int           `json:",default=1048576"`
	MaxBodyBytes    int64         `json:",default=1048576"`
	Middlewares     MiddlewaresConf
	// TraceIgnorePaths skips spans for these request paths.
	TraceIgnorePaths []string `json:",optional"`
	// LogSkipPaths omits these request paths from the access log.
	LogSkipPaths []string `json:",optional"`
}

// MiddlewaresConf controls server-wide middleware defaults.
type MiddlewaresConf struct {
	Recovery  bool          `json:",default=true"`
	RequestID bool          `json:",default=true"`
	Trace     bool          `json:",default=false"`
	Log       bool          `json:",default=true"`
	MaxConns  int           `json:",default=0"`
	Timeout   time.Duration `json:",default=0"`
	Gunzip    bool          `json:",default=false"`
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
		MaxBodyBytes:    1 << 20,
		Middlewares: MiddlewaresConf{
			Recovery:  true,
			RequestID: true,
			Log:       true,
		},
	}
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
	if c.MaxBodyBytes < 0 {
		return fmt.Errorf("rest: max body bytes must not be negative: %d", c.MaxBodyBytes)
	}
	if c.ReadTimeout < 0 || c.WriteTimeout < 0 || c.IdleTimeout < 0 || c.ShutdownTimeout < 0 {
		return errors.New("rest: server timeouts must not be negative")
	}
	return nil
}
