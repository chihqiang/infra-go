package websocket

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// --- Default constants ---

const (
	// roomTypeRedis is the Redis room type (used in switch statements).
	roomTypeRedis = "redis"
)

// Config is the WebSocket server configuration.
// Default values are defined by `default` struct tags, following the conf standard.
// Zero-value fields are filled with defaults automatically in New.
type Config struct {
	// PingInterval is the heartbeat interval, 25 seconds by default.
	// The server sends a Ping frame to the client at this interval and the client must
	// reply with a Pong within PingTimeout.
	PingInterval time.Duration `json:",default=25s"`
	// PingTimeout is the heartbeat timeout, 60 seconds by default.
	// The connection is closed when no client message or Pong arrives within this time.
	PingTimeout time.Duration `json:",default=60s"`
	// ReadBufferSize is the read buffer size in bytes, 4096 by default.
	ReadBufferSize int `json:",default=4096"`
	// WriteBufferSize is the write buffer size in bytes, 4096 by default.
	WriteBufferSize int `json:",default=4096"`
	// WriteTimeout is the timeout of a single write operation, 10 seconds by default.
	//
	// A write deadline is set before every write. Without this limit, a client whose
	// write buffer is full and that does not read data would make WriteMessage block
	// forever while holding the write lock of the connection, which in turn blocks
	// broadcasts, Conn.Close, the heartbeat goroutine and Server.Close.
	// A negative value disables the write timeout (not recommended, only for
	// compatibility with extreme cases).
	WriteTimeout time.Duration `json:",default=10s"`
	// MaxMessageSize is the maximum size of a single message in bytes, 4096 by default.
	// Messages larger than this are rejected.
	MaxMessageSize int64 `json:",default=4096"`
	// NodeID is the node ID, used to distinguish instances in a cluster deployment;
	// 0 by default.
	// In a cluster deployment every instance must use a different NodeID (0~65535);
	// the connection ID is encoded as nodeID<<32 | localCounter to stay globally unique.
	// Keep the default 0 for a standalone deployment.
	NodeID uint16 `json:",optional"`
	// RoomType is the room storage type; "memory" and "redis" are supported,
	// "memory" by default.
	// memory fits a standalone deployment, redis fits a multi-instance deployment.
	RoomType string `json:",default=memory"`
	// RoomPrefix is the Redis room key prefix, "ws:room:" by default.
	// It only takes effect when RoomType is "redis".
	RoomPrefix string `json:",default=ws:room:"`
	// RedisAddr is the Redis address, "127.0.0.1:6379" by default.
	// It is only used when RoomType is "redis" and no client is passed through
	// WithRedisClient.
	RedisAddr string `json:",default=127.0.0.1:6379"`
	// RedisPassword is the Redis password, empty by default.
	RedisPassword string `json:",optional"`
	// RedisDB is the Redis database number, 0 by default.
	RedisDB int `json:",optional"`
}

// fillDefaultUnmarshaler is the unmarshaler used to fill in default values.
var fillDefaultUnmarshaler = mapping.NewDefaultUnmarshaler()

// fillDefault fills in the default values, then overrides them with the non-zero
// fields from the user configuration.
func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	if cfg.PingInterval != 0 {
		c.PingInterval = cfg.PingInterval
	}
	if cfg.PingTimeout != 0 {
		c.PingTimeout = cfg.PingTimeout
	}
	if cfg.ReadBufferSize != 0 {
		c.ReadBufferSize = cfg.ReadBufferSize
	}
	if cfg.WriteBufferSize != 0 {
		c.WriteBufferSize = cfg.WriteBufferSize
	}
	if cfg.WriteTimeout != 0 {
		c.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.MaxMessageSize != 0 {
		c.MaxMessageSize = cfg.MaxMessageSize
	}
	c.NodeID = cfg.NodeID
	if cfg.RoomType != "" {
		c.RoomType = cfg.RoomType
	}
	if cfg.RoomPrefix != "" {
		c.RoomPrefix = cfg.RoomPrefix
	}
	if cfg.RedisAddr != "" {
		c.RedisAddr = cfg.RedisAddr
	}
	c.RedisPassword = cfg.RedisPassword
	if cfg.RedisDB != 0 {
		c.RedisDB = cfg.RedisDB
	}

	return c
}
