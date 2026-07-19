package orm

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// Driver 数据库驱动类型。
type Driver string

const (
	// DriverMySQL MySQL 驱动。
	DriverMySQL Driver = "mysql"
	// DriverPostgres PostgreSQL 驱动。
	DriverPostgres Driver = "postgres"
	// DriverSQLite SQLite 驱动。
	DriverSQLite Driver = "sqlite"
)

// LogLevel GORM 日志级别类型。
type LogLevel int

const (
	// LogSilent 静默模式，不输出任何日志。
	LogSilent LogLevel = 1
	// LogError 仅输出错误日志。
	LogError LogLevel = 2
	// LogWarn 输出警告及以上级别日志。
	LogWarn LogLevel = 3
	// LogInfo 输出所有日志（包括 SQL）。
	LogInfo LogLevel = 4
)

// Config 数据库配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
// 零值字段在 New 时会自动填充默认值。
type Config struct {
	// Driver 数据库驱动类型，支持 "mysql"、"postgres"、"sqlite"，必填。
	Driver Driver `json:"driver"`

	// DSN 数据源名称，完整连接字符串。
	// 如果设置了 DSN，将优先使用 DSN 连接，忽略 Host/Port/Username/Password/Database 字段。
	// 例如 MySQL: "user:password@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=true"
	// 例如 Postgres: "host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable"
	// 例如 SQLite: "file::memory:?cache=shared"
	DSN string `json:",optional"`

	// Host 数据库主机地址，默认 "127.0.0.1"。
	Host string `json:",default=127.0.0.1"`
	// Port 数据库端口。
	// MySQL 默认 3306，Postgres 默认 5432，SQLite 默认 0。
	Port int `json:",optional"`
	// Username 数据库用户名，默认 "root"。
	Username string `json:",default=root"`
	// Password 数据库密码，默认空。
	Password string `json:",optional"`
	// Database 数据库名称（SQLite 为文件路径），默认 ""。
	Database string `json:",optional"`

	// MaxIdleConns 最大空闲连接数，默认 10。
	MaxIdleConns int `json:",default=10"`
	// MaxOpenConns 最大打开连接数，默认 100。
	MaxOpenConns int `json:",default=100"`
	// ConnMaxLifetime 连接最大存活时间，默认 30 分钟。
	ConnMaxLifetime time.Duration `json:",default=30m"`
	// ConnMaxIdleTime 连接最大空闲时间，默认 10 分钟。
	ConnMaxIdleTime time.Duration `json:",default=10m"`

	// LogLevel GORM 日志级别，默认 LogWarn。
	LogLevel LogLevel `json:",default=3"`
	// SlowThreshold 慢查询阈值，超过此时间的 SQL 会被记录为慢查询，默认 200 毫秒。
	SlowThreshold time.Duration `json:",default=200ms"`
	// Colorful 是否启用彩色日志输出，默认 false。
	Colorful bool `json:",optional"`
	// SkipDefaultTransaction 是否跳过默认事务，默认 true。
	// 开启后普通写入操作不会自动包装在事务中，可提升约 30% 性能。
	SkipDefaultTransaction bool `json:",default=true"`
	// AutoPrefix 是否自动添加表名前缀，默认 false。
	AutoPrefix bool `json:",optional"`
	// TablePrefix 表名前缀，默认空。
	TablePrefix string `json:",optional"`
	// SingularTable 是否使用单数表名，默认 false。
	SingularTable bool `json:",optional"`
}

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	// 用用户配置中的非零字段覆盖默认值
	if cfg.Driver != "" {
		c.Driver = cfg.Driver
	}
	// DSN：始终使用用户值（空字符串也是有效值）
	c.DSN = cfg.DSN
	if cfg.Host != "" {
		c.Host = cfg.Host
	}
	if cfg.Port != 0 {
		c.Port = cfg.Port
	}
	if cfg.Username != "" {
		c.Username = cfg.Username
	}
	// Password：始终使用用户值
	c.Password = cfg.Password
	// Database：始终使用用户值
	c.Database = cfg.Database
	if cfg.MaxIdleConns != 0 {
		c.MaxIdleConns = cfg.MaxIdleConns
	}
	if cfg.MaxOpenConns != 0 {
		c.MaxOpenConns = cfg.MaxOpenConns
	}
	if cfg.ConnMaxLifetime != 0 {
		c.ConnMaxLifetime = cfg.ConnMaxLifetime
	}
	if cfg.ConnMaxIdleTime != 0 {
		c.ConnMaxIdleTime = cfg.ConnMaxIdleTime
	}
	if cfg.LogLevel != 0 {
		c.LogLevel = cfg.LogLevel
	}
	if cfg.SlowThreshold != 0 {
		c.SlowThreshold = cfg.SlowThreshold
	}
	if cfg.Colorful {
		c.Colorful = cfg.Colorful
	}
	if cfg.SkipDefaultTransaction {
		c.SkipDefaultTransaction = cfg.SkipDefaultTransaction
	}
	if cfg.AutoPrefix {
		c.AutoPrefix = cfg.AutoPrefix
	}
	if cfg.TablePrefix != "" {
		c.TablePrefix = cfg.TablePrefix
	}
	if cfg.SingularTable {
		c.SingularTable = cfg.SingularTable
	}

	return c
}

// defaultPort 返回指定驱动的默认端口。
func defaultPort(driver Driver) int {
	switch driver {
	case DriverMySQL:
		return 3306
	case DriverPostgres:
		return 5432
	default:
		return 0
	}
}
