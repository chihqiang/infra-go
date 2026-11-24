package orm

import (
	"fmt"

	"gorm.io/gorm"
)

// New creates and returns a *gorm.DB from the configuration.
// The database driver is selected automatically from Config.Driver.
// Zero-valued fields are filled in with their defaults (declared with the default tag).
func New(cfg Config) (*gorm.DB, error) {
	return NewDB(cfg).Open()
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
