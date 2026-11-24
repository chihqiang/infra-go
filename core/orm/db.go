package orm

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chihqiang/ten-go/core/mapping"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// sqliteMemCounter generates a unique sequence number for every DB instance.
// It must be shared across instances: a per-instance counter would hand out the
// same name to independent in-memory databases and make them share one store.
var sqliteMemCounter atomic.Uint64

// DB holds the resolved configuration and opens the configured driver.
// Create one with NewDB, then call OpenMySQL, OpenPostgres or OpenSQLite.
type DB struct {
	cfg Config
	seq uint64
}

// NewDB fills in defaults and returns a DB handle.
// Use OpenMySQL, OpenPostgres or OpenSQLite to open a specific driver,
// or Open/New to let the driver be selected from Config.Driver.
func NewDB(cfg Config) *DB {
	d := &DB{cfg: cfg, seq: sqliteMemCounter.Add(1)}
	d.fillDefault()
	return d
}

// fillDefault fills in the default values and then overrides them with the non-zero fields
// of the user configuration, using mapping.FillAndOverride for both steps.
// An empty DSN, Password or Database is also treated as a valid value (it always
// overrides); this is achieved by the optional tag with no default (in shouldOverride a
// string that is optional and has no default always overrides).
func (d *DB) fillDefault() {
	var c Config
	mapping.MustFillAndOverride(&c, d.cfg)
	d.cfg = c
}

// defaultPort returns the default port of the configured driver.
func (d *DB) defaultPort() int {
	switch d.cfg.Driver {
	case DriverMySQL:
		return 3306
	case DriverPostgres:
		return 5432
	default:
		return 0
	}
}

// useDriver forces the driver and applies its default port when none is set.
func (d *DB) useDriver(driver Driver) {
	d.cfg.Driver = driver
	if d.cfg.Port == 0 {
		d.cfg.Port = d.defaultPort()
	}
}

// adjustSQLitePool configures the pool for in-memory SQLite databases. Without
// this, the pool opens extra connections that see a different (empty) database,
// or the last idle connection is closed and the whole database disappears.
func (d *DB) adjustSQLitePool() {
	if memory, shared := d.sqliteMemoryMode(); memory {
		if shared {
			// cache=shared databases survive while at least one connection is open,
			// so connections must never be recycled or closed for being idle.
			d.cfg.ConnMaxIdleTime = 0
			d.cfg.ConnMaxLifetime = 0
			if d.cfg.MaxIdleConns < 1 {
				d.cfg.MaxIdleConns = 1
			}
		} else {
			// ":memory:" (and mode=memory without cache=shared) is per connection:
			// a second pooled connection would see an empty database.
			d.cfg.MaxOpenConns = 1
			d.cfg.MaxIdleConns = 1
			d.cfg.ConnMaxIdleTime = 0
			d.cfg.ConnMaxLifetime = 0
		}
	}
}

// Open opens the connection for the configured driver.
func (d *DB) Open() (*gorm.DB, error) {
	switch d.cfg.Driver {
	case DriverMySQL:
		return d.OpenMySQL()
	case DriverPostgres:
		return d.OpenPostgres()
	case DriverSQLite:
		return d.OpenSQLite()
	default:
		return nil, fmt.Errorf("unsupported database driver: %s, supported: mysql, postgres, sqlite", d.cfg.Driver)
	}
}

// OpenMySQL opens a MySQL connection.
func (d *DB) OpenMySQL() (*gorm.DB, error) {
	d.useDriver(DriverMySQL)
	return d.open(d.mysqlDialector())
}

// OpenPostgres opens a PostgreSQL connection.
func (d *DB) OpenPostgres() (*gorm.DB, error) {
	d.useDriver(DriverPostgres)
	return d.open(d.postgresDialector())
}

// OpenSQLite opens a SQLite connection.
func (d *DB) OpenSQLite() (*gorm.DB, error) {
	d.useDriver(DriverSQLite)
	d.adjustSQLitePool()
	return d.open(d.sqliteDialector())
}

// mysqlDialector builds the MySQL dialector, preferring DSN over split fields.
func (d *DB) mysqlDialector() gorm.Dialector {
	if d.cfg.DSN != "" {
		return mysql.Open(d.cfg.DSN)
	}
	return mysql.Open(d.mysqlDSN())
}

// postgresDialector builds the PostgreSQL dialector, preferring DSN over split fields.
func (d *DB) postgresDialector() gorm.Dialector {
	if d.cfg.DSN != "" {
		return postgres.Open(d.cfg.DSN)
	}
	return postgres.Open(d.postgresDSN())
}

// sqliteDialector builds the SQLite dialector, preferring DSN over split fields.
func (d *DB) sqliteDialector() gorm.Dialector {
	if d.cfg.DSN != "" {
		return sqlite.Open(d.cfg.DSN)
	}
	return sqlite.Open(d.sqliteDSN())
}

// open creates the gorm.DB and configures the connection pool.
func (d *DB) open(dialector gorm.Dialector) (*gorm.DB, error) {
	gormCfg := d.gormConfig()

	db, err := gorm.Open(dialector, &gormCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure the connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(d.cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(d.cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(d.cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(d.cfg.ConnMaxIdleTime)

	return db, nil
}

// mysqlDSN builds the MySQL connection string.
//
// The DSN is built through the driver's own Config.FormatDSN instead of
// fmt.Sprintf: characters such as '@', '/', ':' or spaces in a username or
// password are escaped correctly, while a hand-written string silently produced
// a broken DSN (the driver splits the address at the last '@').
func (d *DB) mysqlDSN() string {
	cfg := mysqldriver.NewConfig()
	cfg.User = d.cfg.Username
	cfg.Passwd = d.cfg.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(d.cfg.Host, strconv.Itoa(d.cfg.Port))
	cfg.DBName = d.cfg.Database
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg.FormatDSN()
}

// postgresDSN builds the PostgreSQL connection string, in the format
// host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable TimeZone=Asia/Shanghai
// An empty SSLMode falls back to "disable" and an empty TimeZone falls back to
// "Asia/Shanghai", so a Config built directly (without NewDB) still yields a valid DSN.
func (d *DB) postgresDSN() string {
	sslMode := d.cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	timeZone := d.cfg.TimeZone
	if timeZone == "" {
		timeZone = "Asia/Shanghai"
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		d.pgDSNValue(d.cfg.Host), d.pgDSNValue(d.cfg.Username), d.pgDSNValue(d.cfg.Password),
		d.pgDSNValue(d.cfg.Database), d.cfg.Port, d.pgDSNValue(sslMode), d.pgDSNValue(timeZone))
}

// pgDSNValue quotes a value for a libpq keyword/value connection string. Values
// containing spaces, quotes or backslashes have to be single-quoted with the
// special characters escaped, otherwise a password such as "p a'ss" produces an
// unparsable DSN.
func (d *DB) pgDSNValue(value string) string {
	if value != "" && !strings.ContainsAny(value, ` '\\`) {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}

// sqliteMemoryMode reports whether the configuration describes an in-memory SQLite
// database, and whether it is shared between connections (cache=shared).
//
// An empty Database produces the generated DSN from sqliteDSN, which uses
// cache=shared. A plain ":memory:" is per connection.
func (d *DB) sqliteMemoryMode() (memory, shared bool) {
	if d.cfg.Driver != DriverSQLite {
		return false, false
	}
	dsn := strings.TrimSpace(strings.ToLower(d.cfg.DSN))
	if dsn == "" {
		dsn = strings.TrimSpace(strings.ToLower(d.cfg.Database))
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

// sqliteDSN builds the SQLite connection string.
// Config.Database is the file path; when it is empty an in-memory database **exclusive to
// this instance** is used.
//
// Exclusivity matters: the old implementation always returned "file::memory?cache=shared",
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
func (d *DB) sqliteDSN() string {
	if d.cfg.Database == "" {
		return fmt.Sprintf("file:orm_mem_%d_%d?mode=memory&cache=shared",
			os.Getpid(), d.seq)
	}
	return d.cfg.Database
}

// gormConfig builds the gorm.Config.
func (d *DB) gormConfig() gorm.Config {
	return gorm.Config{
		SkipDefaultTransaction: d.cfg.SkipDefaultTransaction,
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   d.cfg.TablePrefix,
			SingularTable: d.cfg.SingularTable,
		},
		Logger:         buildGormLogger(d.cfg),
		TranslateError: d.cfg.TranslateError,
	}
}
