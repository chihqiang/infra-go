package redisx

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// --- 默认常量 ---

const (
	// defaultAddr 默认 Redis 地址。
	defaultAddr = "127.0.0.1:6379"
	// defaultPoolSize 默认连接池大小。
	defaultPoolSize = 10
	// defaultMinIdleConns 默认最小空闲连接数。
	defaultMinIdleConns = 2
	// defaultMaxRetries 默认命令最大重试次数。
	defaultMaxRetries = 3
	// defaultDialTimeout 默认连接超时。
	defaultDialTimeout = 5 * time.Second
	// defaultReadTimeout 默认读取超时。
	defaultReadTimeout = 3 * time.Second
	// defaultWriteTimeout 默认写入超时。
	defaultWriteTimeout = 3 * time.Second
	// defaultPoolTimeout 默认连接池获取超时。
	defaultPoolTimeout = 4 * time.Second
	// defaultIdleConnTimeout 默认空闲连接超时。
	defaultIdleConnTimeout = 5 * time.Minute
	// defaultConnMaxIdleTime 默认连接最大空闲时间。
	defaultConnMaxIdleTime = 5 * time.Minute
)

// Config Redis 配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
// 零值字段在 New 时会自动填充默认值。
type Config struct {
	// Addr Redis 服务器地址，默认 "127.0.0.1:6379"。
	Addr string `json:",default=127.0.0.1:6379"`
	// Username Redis 用户名（Redis 6.0+ ACL），默认空。
	Username string `json:",optional"`
	// Password Redis 密码，默认空。
	Password string `json:",optional"`
	// DB Redis 数据库编号，默认 0。
	DB int `json:",optional"`
	// MasterName 哨兵模式下的主节点名称，设置后启用哨兵模式，默认空。
	MasterName string `json:",optional"`
	// SentinelAddrs 哨兵节点地址列表，默认空。
	SentinelAddrs []string `json:",optional"`

	// PoolSize 连接池大小，默认 10。
	PoolSize int `json:",default=10"`
	// MinIdleConns 最小空闲连接数，默认 2。
	MinIdleConns int `json:",default=2"`
	// MaxRetries 命令最大重试次数，默认 3。
	MaxRetries int `json:",default=3"`
	// DialTimeout 连接超时时间，默认 5 秒。
	DialTimeout time.Duration `json:",default=5s"`
	// ReadTimeout 读取超时时间，默认 3 秒。
	ReadTimeout time.Duration `json:",default=3s"`
	// WriteTimeout 写入超时时间，默认 3 秒。
	WriteTimeout time.Duration `json:",default=3s"`
	// PoolTimeout 连接池获取连接超时时间，默认 ReadTimeout + 1 秒。
	PoolTimeout time.Duration `json:",default=4s"`
	// IdleConnTimeout 空闲连接超时时间，超过此时间的连接会被关闭，默认 5 分钟。
	IdleConnTimeout time.Duration `json:",default=5m"`
	// MaxIdleConnsToCheck 定期检查空闲连接的最大数量，默认 0（不检查）。
	ConnMaxIdleTime time.Duration `json:",default=5m"`

	// KeyPrefix 键名前缀，所有操作会自动添加此前缀，默认空。
	KeyPrefix string `json:",optional"`
}

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	if cfg.Addr != "" {
		c.Addr = cfg.Addr
	}
	// Username：空字符串是有效值
	c.Username = cfg.Username
	// Password：空字符串是有效值
	c.Password = cfg.Password
	if cfg.DB != 0 {
		c.DB = cfg.DB
	}
	if cfg.MasterName != "" {
		c.MasterName = cfg.MasterName
	}
	if len(cfg.SentinelAddrs) > 0 {
		c.SentinelAddrs = cfg.SentinelAddrs
	}
	if cfg.PoolSize != 0 {
		c.PoolSize = cfg.PoolSize
	}
	if cfg.MinIdleConns != 0 {
		c.MinIdleConns = cfg.MinIdleConns
	}
	if cfg.MaxRetries != 0 {
		c.MaxRetries = cfg.MaxRetries
	}
	if cfg.DialTimeout != 0 {
		c.DialTimeout = cfg.DialTimeout
	}
	if cfg.ReadTimeout != 0 {
		c.ReadTimeout = cfg.ReadTimeout
	}
	if cfg.WriteTimeout != 0 {
		c.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.PoolTimeout != 0 {
		c.PoolTimeout = cfg.PoolTimeout
	}
	if cfg.IdleConnTimeout != 0 {
		c.IdleConnTimeout = cfg.IdleConnTimeout
	}
	if cfg.ConnMaxIdleTime != 0 {
		c.ConnMaxIdleTime = cfg.ConnMaxIdleTime
	}
	// KeyPrefix：空字符串是有效值
	c.KeyPrefix = cfg.KeyPrefix

	return c
}
