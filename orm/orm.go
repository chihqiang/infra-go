package orm

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// New 根据配置创建并返回一个 *gorm.DB 实例。
// 根据 Config.Driver 自动选择对应的数据库驱动。
// 零值字段会自动填充默认值（通过 default 标签定义）。
func New(cfg Config) (*gorm.DB, error) {
	c := fillDefault(cfg)

	// 端口未设置时使用驱动默认端口
	if c.Port == 0 {
		c.Port = defaultPort(c.Driver)
	}

	dialector, err := buildDialector(c)
	if err != nil {
		return nil, err
	}

	gormCfg := buildGormConfig(c)

	db, err := gorm.Open(dialector, &gormCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(c.MaxIdleConns)
	sqlDB.SetMaxOpenConns(c.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(c.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(c.ConnMaxIdleTime)

	return db, nil
}

// MustNew 根据配置创建并返回一个 *gorm.DB 实例，出错时 panic。
func MustNew(cfg Config) *gorm.DB {
	db, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create database connection: %w", err))
	}
	return db
}

// NewMySQL 创建一个 MySQL 数据库连接。
// 忽略 Config.Driver 字段，强制使用 MySQL 驱动。
func NewMySQL(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverMySQL
	return New(cfg)
}

// MustNewMySQL 创建一个 MySQL 数据库连接，出错时 panic。
func MustNewMySQL(cfg Config) *gorm.DB {
	db, err := NewMySQL(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create MySQL connection: %w", err))
	}
	return db
}

// NewPostgres 创建一个 PostgreSQL 数据库连接。
// 忽略 Config.Driver 字段，强制使用 PostgreSQL 驱动。
func NewPostgres(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverPostgres
	return New(cfg)
}

// MustNewPostgres 创建一个 PostgreSQL 数据库连接，出错时 panic。
func MustNewPostgres(cfg Config) *gorm.DB {
	db, err := NewPostgres(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create PostgreSQL connection: %w", err))
	}
	return db
}

// NewSQLite 创建一个 SQLite 数据库连接。
// 忽略 Config.Driver 字段，强制使用 SQLite 驱动。
// Config.Database 为数据库文件路径，设为 ":memory:" 使用内存数据库。
func NewSQLite(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverSQLite
	return New(cfg)
}

// MustNewSQLite 创建一个 SQLite 数据库连接，出错时 panic。
func MustNewSQLite(cfg Config) *gorm.DB {
	db, err := NewSQLite(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create SQLite connection: %w", err))
	}
	return db
}

// Ping 测试数据库连接是否正常。
func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

// Close 关闭数据库连接。
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// --- 内部函数 ---

// buildDialector 根据配置构建对应驱动的 Dialector。
func buildDialector(c Config) (gorm.Dialector, error) {
	// 优先使用 DSN
	if c.DSN != "" {
		return dialectorFromDSN(c.Driver, c.DSN)
	}

	switch c.Driver {
	case DriverMySQL:
		return mysql.Open(buildMySQLDSN(c)), nil
	case DriverPostgres:
		return postgres.Open(buildPostgresDSN(c)), nil
	case DriverSQLite:
		return sqlite.Open(buildSQLiteDSN(c)), nil
	default:
		return nil, fmt.Errorf("unsupported database driver: %s, supported: mysql, postgres, sqlite", c.Driver)
	}
}

// dialectorFromDSN 根据 DSN 创建对应驱动的 Dialector。
// 非法 Driver 返回错误，避免静默降级为其他驱动导致难以排查的问题。
func dialectorFromDSN(driver Driver, dsn string) (gorm.Dialector, error) {
	switch driver {
	case DriverMySQL:
		return mysql.Open(dsn), nil
	case DriverPostgres:
		return postgres.Open(dsn), nil
	case DriverSQLite:
		return sqlite.Open(dsn), nil
	default:
		return nil, fmt.Errorf("unsupported database driver: %s, supported: mysql, postgres, sqlite", driver)
	}
}

// buildMySQLDSN 构建 MySQL 连接字符串。
// 格式: username:password@tcp(host:port)/database?charset=utf8mb4&parseTime=true&loc=Local
func buildMySQLDSN(c Config) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		c.Username, c.Password, c.Host, c.Port, c.Database)
}

// buildPostgresDSN 构建 PostgreSQL 连接字符串。
// 格式: host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable TimeZone=Asia/Shanghai
// SSLMode 为空时回退到 "disable"，保证未经过 fillDefault 直接构造的 Config 也能得到合法 DSN。
func buildPostgresDSN(c Config) string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		c.Host, c.Username, c.Password, c.Database, c.Port, sslMode)
}

// buildSQLiteDSN 构建 SQLite 连接字符串。
// Config.Database 为文件路径，为空时使用内存数据库。
func buildSQLiteDSN(c Config) string {
	if c.Database == "" {
		return "file::memory:?cache=shared"
	}
	return c.Database
}

// buildGormConfig 构建 gorm.Config。
func buildGormConfig(c Config) gorm.Config {
	return gorm.Config{
		SkipDefaultTransaction: c.SkipDefaultTransaction,
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   c.TablePrefix,
			SingularTable: c.SingularTable,
		},
		Logger: buildGormLogger(c),
	}
}
