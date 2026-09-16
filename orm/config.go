package orm

import (
	"time"

	"github.com/chihqiang/infra-go/mapping"
)

// Driver is the database driver type.
type Driver string

const (
	// DriverMySQL is the MySQL driver.
	DriverMySQL Driver = "mysql"
	// DriverPostgres is the PostgreSQL driver.
	DriverPostgres Driver = "postgres"
	// DriverSQLite is the SQLite driver.
	DriverSQLite Driver = "sqlite"
)

// LogLevel is the GORM log level type.
type LogLevel int

const (
	// LogSilent is silent mode, which outputs no logs at all.
	LogSilent LogLevel = 1
	// LogError only outputs error logs.
	LogError LogLevel = 2
	// LogWarn outputs warnings and above.
	LogWarn LogLevel = 3
	// LogInfo outputs all logs (including SQL).
	LogInfo LogLevel = 4
)

// Config is the database configuration.
// Default values are declared with the struct tag default, following the conf standard.
// Zero-valued fields are filled in with their defaults by New.
type Config struct {
	// Driver is the database driver type; "mysql", "postgres" and "sqlite" are
	// supported. It is required.
	Driver Driver `json:"driver"`

	// DSN is the data source name, a complete connection string.
	// When DSN is set it takes precedence and the Host/Port/Username/Password/Database
	// fields are ignored.
	// For example MySQL: "user:password@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=true"
	// For example Postgres: "host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable"
	// For example SQLite: "file::memory:?cache=shared"
	DSN string `json:",optional"`

	// Host is the database host address; defaults to "127.0.0.1".
	Host string `json:",default=127.0.0.1"`
	// Port is the database port.
	// MySQL defaults to 3306, Postgres to 5432 and SQLite to 0.
	Port int `json:",optional"`
	// Username is the database user name; defaults to "root".
	Username string `json:",default=root"`
	// Password is the database password; defaults to empty.
	Password string `json:",optional"`
	// Database is the database name (for SQLite it is the file path); defaults to "".
	Database string `json:",optional"`

	// SSLMode is the PostgreSQL SSL mode; defaults to "disable".
	// Possible values: disable, allow, prefer, require, verify-ca, verify-full.
	// For production it is recommended to set "require" or stricter to enable encrypted
	// connections.
	SSLMode string `json:",default=disable"`

	// TimeZone is the database session time zone; defaults to "Asia/Shanghai".
	// It affects how timestamps are interpreted when connecting to PostgreSQL.
	// Common values: UTC, Asia/Shanghai, America/New_York, etc.
	TimeZone string `json:",default=Asia/Shanghai"`

	// MaxIdleConns is the maximum number of idle connections; defaults to 10.
	MaxIdleConns int `json:",default=10"`
	// MaxOpenConns is the maximum number of open connections; defaults to 100.
	MaxOpenConns int `json:",default=100"`
	// ConnMaxLifetime is the maximum lifetime of a connection; defaults to 30 minutes.
	ConnMaxLifetime time.Duration `json:",default=30m"`
	// ConnMaxIdleTime is the maximum idle time of a connection; defaults to 10 minutes.
	ConnMaxIdleTime time.Duration `json:",default=10m"`

	// LogLevel is the GORM log level; defaults to LogWarn.
	LogLevel LogLevel `json:",default=3"`
	// SlowThreshold is the slow query threshold: SQL taking longer than this is recorded
	// as a slow query. It defaults to 200 milliseconds.
	SlowThreshold time.Duration `json:",default=200ms"`
	// Colorful reports whether colourful log output is enabled; defaults to false.
	Colorful bool `json:",optional"`
	// SkipDefaultTransaction reports whether the default transaction is skipped; defaults
	// to true.
	// When enabled, ordinary write operations are not automatically wrapped in a
	// transaction, which can improve performance by roughly 30%.
	SkipDefaultTransaction bool `json:",default=true"`
	// TablePrefix is the table name prefix; defaults to empty.
	TablePrefix string `json:",optional"`
	// SingularTable reports whether singular table names are used; defaults to false.
	SingularTable bool `json:",optional"`
}

// fillDefault fills in the default values and then overrides them with the non-zero fields
// of the user configuration, using mapping.FillAndOverride for both steps.
// An empty DSN, Password or Database is also treated as a valid value (it always
// overrides); this is achieved by the optional tag with no default (in shouldOverride a
// string that is optional and has no default always overrides).
func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	return c
}

// defaultPort returns the default port of the given driver.
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
