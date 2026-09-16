package redisx

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// Config is the Redis configuration.
// Default values are defined through the default struct tag, following the conf
// standard. Zero-value fields are filled with their defaults automatically in New.
type Config struct {
	// Addr is the Redis server address, default "127.0.0.1:6379".
	Addr string `json:",default=127.0.0.1:6379"`
	// Username is the Redis user name (Redis 6.0+ ACL), empty by default.
	Username string `json:",optional"`
	// Password is the Redis password, empty by default.
	Password string `json:",optional"`
	// DB is the Redis database number, default 0.
	DB int `json:",optional"`
	// MasterName is the master node name in sentinel mode; setting it enables sentinel
	// mode. Empty by default.
	MasterName string `json:",optional"`
	// SentinelAddrs is the list of sentinel node addresses, empty by default.
	SentinelAddrs []string `json:",optional"`

	// PoolSize is the connection pool size, default 10.
	PoolSize int `json:",default=10"`
	// MinIdleConns is the minimum number of idle connections, default 2.
	MinIdleConns int `json:",default=2"`
	// MaxRetries is the maximum number of command retries, default 3.
	MaxRetries int `json:",default=3"`
	// DialTimeout is the dial timeout, default 5 seconds.
	DialTimeout time.Duration `json:",default=5s"`
	// ReadTimeout is the read timeout, default 3 seconds.
	ReadTimeout time.Duration `json:",default=3s"`
	// WriteTimeout is the write timeout, default 3 seconds.
	WriteTimeout time.Duration `json:",default=3s"`
	// PoolTimeout is the timeout for getting a connection from the pool, default
	// ReadTimeout + 1 second.
	PoolTimeout time.Duration `json:",default=4s"`
	// ConnMaxIdleTime is the maximum idle time of a connection; connections idle for
	// longer than this are closed and reclaimed. Default 5 minutes.
	ConnMaxIdleTime time.Duration `json:",default=5m"`

	// KeyPrefix is the key prefix; every operation adds it automatically. Empty by
	// default.
	KeyPrefix string `json:",optional"`
}

// fillDefault fills in the default values and then overrides them with the non-zero
// fields of the user configuration, all handled uniformly by mapping.FillAndOverride.
// Empty strings for Username, Password and KeyPrefix count as valid values too (they
// always override), which is achieved by the optional tag with no default.
func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	return c
}
