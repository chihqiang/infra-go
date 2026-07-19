package websocket

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// --- 默认常量 ---

const (
	// defaultPingInterval 默认心跳检测间隔。
	defaultPingInterval = 25 * time.Second
	// defaultPingTimeout 默认心跳超时时间。
	defaultPingTimeout = 60 * time.Second
	// defaultBufferSize 默认读写缓冲区大小（字节）。
	defaultBufferSize = 4096
	// defaultMaxMessageSize 默认单条消息最大大小（字节）。
	defaultMaxMessageSize = 4096
	// defaultRoomType 默认房间存储类型。
	defaultRoomType = "memory"
	// roomTypeRedis Redis 房间类型（用于 switch 判断）。
	roomTypeRedis = "redis"
	// defaultRoomPrefix 默认 Redis 房间键前缀。
	defaultRoomPrefix = "ws:room:"
	// defaultRedisAddr 默认 Redis 地址。
	defaultRedisAddr = "127.0.0.1:6379"
)

// Config WebSocket 服务配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
// 零值字段在 New 时会自动填充默认值。
type Config struct {
	// PingInterval 心跳检测间隔，默认 25 秒。
	// 服务器每隔此间隔向客户端发送 Ping 帧，客户端需在 PingTimeout 内回复 Pong。
	PingInterval time.Duration `json:",default=25s"`
	// PingTimeout 心跳超时时间，默认 60 秒。
	// 超过此时间未收到客户端消息或 Pong，则断开连接。
	PingTimeout time.Duration `json:",default=60s"`
	// ReadBufferSize 读缓冲区大小（字节），默认 4096。
	ReadBufferSize int `json:",default=4096"`
	// WriteBufferSize 写缓冲区大小（字节），默认 4096。
	WriteBufferSize int `json:",default=4096"`
	// MaxMessageSize 单条消息最大大小（字节），默认 4096。
	// 超过此大小的消息会被拒绝。
	MaxMessageSize int64 `json:",default=4096"`
	// NodeID 节点 ID，用于集群部署时区分不同实例，默认 0。
	// 集群部署时每个实例必须设置不同的 NodeID（取值范围 0~65535），
	// 连接 ID 编码为 nodeID<<32 | localCounter，保证全局唯一。
	// 单机部署时保持默认值 0 即可。
	NodeID uint16 `json:",optional"`
	// RoomType 房间存储类型，支持 "memory" 和 "redis"，默认 "memory"。
	// memory 适用于单机部署，redis 适用于多实例部署。
	RoomType string `json:",default=memory"`
	// RoomPrefix Redis 房间键前缀，默认 "ws:room:"。
	// 仅在 RoomType 为 "redis" 时生效。
	RoomPrefix string `json:",default=ws:room:"`
	// RedisAddr Redis 地址，默认 "127.0.0.1:6379"。
	// 仅在 RoomType 为 "redis" 且未通过 WithRedisClient 传入客户端时使用。
	RedisAddr string `json:",default=127.0.0.1:6379"`
	// RedisPassword Redis 密码，默认空。
	RedisPassword string `json:",optional"`
	// RedisDB Redis 数据库编号，默认 0。
	RedisDB int `json:",optional"`
}

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
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
