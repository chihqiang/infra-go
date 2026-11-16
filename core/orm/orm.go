package orm

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// New creates and returns a *gorm.DB from the configuration.
// The database driver is selected automatically from Config.Driver.
// Zero-valued fields are filled in with their defaults (declared with the default tag).
func New(cfg Config) (*gorm.DB, error) {
	c := fillDefault(cfg)

	// Use the driver default port when no port is set
	if c.Port == 0 {
		c.Port = defaultPort(c.Driver)
	}

	// In-memory SQLite databases need a dedicated pool configuration; without it
	// the pool opens extra connections that see a different (empty) database, or
	// the last idle connection is closed and the whole database disappears.
	if memory, shared := sqliteMemoryMode(c); memory {
		if shared {
			// cache=shared databases survive while at least one connection is open,
			// so connections must never be recycled or closed for being idle.
			c.ConnMaxIdleTime = 0
			c.ConnMaxLifetime = 0
			if c.MaxIdleConns < 1 {
				c.MaxIdleConns = 1
			}
		} else {
			// ":memory:" (and mode=memory without cache=shared) is per connection:
			// a second pooled connection would see an empty database.
			c.MaxOpenConns = 1
			c.MaxIdleConns = 1
			c.ConnMaxIdleTime = 0
			c.ConnMaxLifetime = 0
		}
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

	// Configure the connection pool
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

// MustNew creates and returns a *gorm.DB from the configuration and panics on error.
func MustNew(cfg Config) *gorm.DB {
	db, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create database connection: %w", err))
	}
	return db
}

// NewMySQL creates a MySQL database connection.
// The Config.Driver field is ignored and the MySQL driver is forced.
func NewMySQL(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverMySQL
	return New(cfg)
}

// MustNewMySQL creates a MySQL database connection and panics on error.
func MustNewMySQL(cfg Config) *gorm.DB {
	db, err := NewMySQL(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create MySQL connection: %w", err))
	}
	return db
}

// NewPostgres creates a PostgreSQL database connection.
// The Config.Driver field is ignored and the PostgreSQL driver is forced.
func NewPostgres(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverPostgres
	return New(cfg)
}

// MustNewPostgres creates a PostgreSQL database connection and panics on error.
func MustNewPostgres(cfg Config) *gorm.DB {
	db, err := NewPostgres(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create PostgreSQL connection: %w", err))
	}
	return db
}

// NewSQLite creates a SQLite database connection.
// The Config.Driver field is ignored and the SQLite driver is forced.
// Config.Database is the database file path. Leave it empty for an isolated
// in-memory database (recommended); ":memory:" also works, but it is scoped to a
// single connection, so New forces the pool to one connection in that case.
func NewSQLite(cfg Config) (*gorm.DB, error) {
	cfg.Driver = DriverSQLite
	return New(cfg)
}

// MustNewSQLite creates a SQLite database connection and panics on error.
func MustNewSQLite(cfg Config) *gorm.DB {
	db, err := NewSQLite(cfg)
	if err != nil {
		panic(fmt.Errorf("orm: failed to create SQLite connection: %w", err))
	}
	return db
}

// Ping tests whether the database connection is healthy.
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

// Close closes the database connection.
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

// --- Internal functions ---

// buildDialector builds the Dialector of the matching driver from the configuration.
func buildDialector(c Config) (gorm.Dialector, error) {
	// Prefer the DSN
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

// dialectorFromDSN creates the Dialector of the matching driver from a DSN.
// An invalid Driver returns an error, avoiding a silent downgrade to another driver that
// would be hard to diagnose.
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

// buildMySQLDSN builds the MySQL connection string.
//
// The DSN is built through the driver's own Config.FormatDSN instead of
// fmt.Sprintf: characters such as '@', '/', ':' or spaces in a username or
// password are escaped correctly, while a hand-written string silently produced
// a broken DSN (the driver splits the address at the last '@').
func buildMySQLDSN(c Config) string {
	cfg := mysqldriver.NewConfig()
	cfg.User = c.Username
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.DBName = c.Database
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg.FormatDSN()
}

// buildPostgresDSN builds the PostgreSQL connection string, in the format
// host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable TimeZone=Asia/Shanghai
// An empty SSLMode falls back to "disable" and an empty TimeZone falls back to
// "Asia/Shanghai", so a Config built directly without fillDefault still yields a valid DSN.
func buildPostgresDSN(c Config) string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	timeZone := c.TimeZone
	if timeZone == "" {
		timeZone = "Asia/Shanghai"
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		pgDSNValue(c.Host), pgDSNValue(c.Username), pgDSNValue(c.Password),
		pgDSNValue(c.Database), c.Port, pgDSNValue(sslMode), pgDSNValue(timeZone))
}

// pgDSNValue quotes a value for a libpq keyword/value connection string. Values
// containing spaces, quotes or backslashes have to be single-quoted with the
// special characters escaped, otherwise a password such as "p a'ss" produces an
// unparsable DSN.
func pgDSNValue(value string) string {
	if value != "" && !strings.ContainsAny(value, ` '\\`) {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}

// sqliteMemCounter generates a unique sequence number for each in-memory database instance.
var sqliteMemCounter atomic.Uint64

// sqliteMemoryMode reports whether the configuration describes an in-memory SQLite
// database, and whether it is shared between connections (cache=shared).
//
// An empty Database produces the generated DSN from buildSQLiteDSN, which uses
// cache=shared. A plain ":memory:" is per connection.
func sqliteMemoryMode(c Config) (memory, shared bool) {
	if c.Driver != DriverSQLite {
		return false, false
	}
	dsn := strings.TrimSpace(strings.ToLower(c.DSN))
	if dsn == "" {
		dsn = strings.TrimSpace(strings.ToLower(c.Database))
	}
	shared = strings.Contains(dsn, "cache=shared")
	switch {
	case dsn == "":
		return true, true
	case dsn == ":memory:":
		return true, false
	case strings.Contains(dsn, "mode=memory"), strings.HasPrefix(dsn, "file::memory:"):
		return true, shared
	default:
		return false, false
	}
}

// buildSQLiteDSN builds the SQLite connection string.
// Config.Database is the file path; when it is empty an in-memory database **exclusive to
// this instance** is used.
//
// Exclusivity matters: the old implementation always returned "file::memory:?cache=shared",
// and a DSN with that name plus cache=shared shares **one single database across the whole
// process**, so two calls to New returned the same database - a table created or data
// written by one component was visible to the other, and everything was lost as soon as the
// process exited (easily mistaken for "data mysteriously disappearing").
//
// A unique database name is therefore generated on every call, which keeps the convenience
// of "an in-memory database with zero configuration" (common in tests) while guaranteeing
// that instances never interfere with each other.
// Note that cache=shared is still kept: an anonymous in-memory database (:memory:) gives
// every pooled connection its own separate database, so other connections cannot see a
// table that was just created - that is the more common pitfall.
func buildSQLiteDSN(c Config) string {
	if c.Database == "" {
		return fmt.Sprintf("file:orm_mem_%d_%d?mode=memory&cache=shared",
			os.Getpid(), sqliteMemCounter.Add(1))
	}
	return c.Database
}

// buildGormConfig builds the gorm.Config.
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
