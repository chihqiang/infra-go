package redisx

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
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
	// ConnMaxIdleTime 连接最大空闲时间，空闲超过此时长的连接会被关闭并回收，默认 5 分钟。
	ConnMaxIdleTime time.Duration `json:",default=5m"`

	// KeyPrefix 键名前缀，所有操作会自动添加此前缀，默认空。
	KeyPrefix string `json:",optional"`
}

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
// 使用 mapping.FillAndOverride 统一处理。
// Username、Password、KeyPrefix 为空字符串时也视为有效值（始终覆盖），
// 这通过标签 optional 且无 default 实现。
func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	return c
}
