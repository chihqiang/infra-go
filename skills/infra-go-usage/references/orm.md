# orm

A database ORM package built on [GORM](https://gorm.io); supports MySQL, PostgreSQL and SQLite with a unified config struct and connection management.

## Features

- **Multiple databases**: MySQL, PostgreSQL, SQLite behind one API
- **Configuration-driven**: Config defines defaults with `default` struct tags, following the conf standard
- **DSN or split fields**: pass a DSN directly, or configure Host/Port/Username/Password as separate fields
- **Connection pool management**: configurable max idle connections, max open connections and connection lifetime
- **Log bridging**: automatically bridges GORM logs to the `logger` package, with no extra configuration
- **Table name strategy**: supports table prefixes and singular table names
- **Convenience functions**: the `MustNew` family panics on error, plus the `Ping` / `Close` helpers
- **Slow query threshold**: configurable slow-query threshold, with slow SQL flagged automatically

## Installation

```bash
go get github.com/chihqiang/infra-go/orm
```

## Quick start

```go
package main

import (
    "github.com/chihqiang/infra-go/orm"
)

type User struct {
    ID    uint   `gorm:"primaryKey"`
    Name  string `gorm:"size:128"`
    Email string `gorm:"uniqueIndex"`
}

func main() {
    // SQLite in-memory database (good for development and testing)
    db := orm.MustNewSQLite(orm.Config{
        Database: ":memory:",
    })

    // automatic migration
    db.AutoMigrate(&User{})

    // create, read, update, delete
    db.Create(&User{Name: "alice", Email: "alice@example.com"})

    var user User
    db.First(&user, 1)
}
```

## Configuration

### The Config struct

```go
db, err := orm.New(orm.Config{
    Driver:          orm.DriverMySQL,
    Host:            "127.0.0.1",
    Port:            3306,
    Username:        "root",
    Password:        "secret",
    Database:        "myapp",
    MaxIdleConns:    10,
    MaxOpenConns:    100,
    ConnMaxLifetime: 30 * time.Minute,
    ConnMaxIdleTime: 10 * time.Minute,
    LogLevel:        orm.LogWarn,
    SlowThreshold:   200 * time.Millisecond,
    SkipDefaultTransaction: true,
})
```

### Config reference

| Field | Type | Default | Description |
| ------ | ------ | -------- | ------ |
| `Driver` | `Driver` | — | Database driver: `mysql`, `postgres`, `sqlite`; required |
| `DSN` | `string` | `""` | Data source name; when set it takes precedence and Host/Port etc. are ignored |
| `Host` | `string` | `127.0.0.1` | Database host address |
| `Port` | `int` | driver default | MySQL 3306, Postgres 5432, SQLite 0 |
| `Username` | `string` | `root` | Database username |
| `Password` | `string` | `""` | Database password |
| `Database` | `string` | `""` | Database name (for SQLite this is the file path; when empty SQLite uses a memory database exclusive to the instance) |
| `SSLMode` | `string` | `disable` | PostgreSQL SSL mode: `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`; `require` is recommended in production |
| `TimeZone` | `string` | `Asia/Shanghai` | Database session time zone; affects how timestamps are interpreted when connecting to PostgreSQL. Common values: `UTC`, `Asia/Shanghai`, `America/New_York`, etc. |
| `MaxIdleConns` | `int` | `10` | Maximum number of idle connections |
| `MaxOpenConns` | `int` | `100` | Maximum number of open connections |
| `ConnMaxLifetime` | `Duration` | `30m` | Maximum lifetime of a connection |
| `ConnMaxIdleTime` | `Duration` | `10m` | Maximum idle time of a connection |
| `LogLevel` | `LogLevel` | `LogWarn` | GORM log level |
| `SlowThreshold` | `Duration` | `200ms` | Slow query threshold |
| `Colorful` | `bool` | `false` | Whether to enable colored log output |
| `SkipDefaultTransaction` | `bool` | `true` | Skip the default transaction (improves write performance by roughly 30%) |
| `TablePrefix` | `string` | `""` | Table name prefix |
| `SingularTable` | `bool` | `false` | Whether to use singular table names |

### Log levels

```go
orm.LogSilent // silent, no log output
orm.LogError  // errors only
orm.LogWarn   // warnings and above (default)
orm.LogInfo   // everything (including SQL)
```

## API

### Generic connection function

```go
// New picks the driver automatically from Config.Driver
db, err := orm.New(cfg)

// MustNew panics on error
db := orm.MustNew(cfg)
```

### Driver-specific connection functions

```go
// MySQL
db, err := orm.NewMySQL(cfg)
db := orm.MustNewMySQL(cfg)

// PostgreSQL
db, err := orm.NewPostgres(cfg)
db := orm.MustNewPostgres(cfg)

// SQLite
db, err := orm.NewSQLite(cfg)
db := orm.MustNewSQLite(cfg)
```

### Helper functions

```go
// test the connection
err := orm.Ping(db)

// close the connection
err := orm.Close(db)
```

## Database examples

### MySQL

```go
// option 1: split fields
db, err := orm.NewMySQL(orm.Config{
    Host:     "127.0.0.1",
    Port:     3306,
    Username: "root",
    Password: "secret",
    Database: "myapp",
})

// option 2: DSN
db, err := orm.NewMySQL(orm.Config{
    DSN: "root:secret@tcp(127.0.0.1:3306)/myapp?charset=utf8mb4&parseTime=true",
})
```

### PostgreSQL

```go
// option 1: split fields
db, err := orm.NewPostgres(orm.Config{
    Host:     "127.0.0.1",
    Port:     5432,
    Username: "postgres",
    Password: "secret",
    Database: "myapp",
    TimeZone: "UTC",  // optional, defaults to Asia/Shanghai
})

// option 2: DSN
// when the DSN already contains a TimeZone, the DSN value wins
// with split fields, the TimeZone field is written into the DSN automatically
```

### SQLite

```go
// in-memory database (good for testing)
db, err := orm.NewSQLite(orm.Config{
    Database: ":memory:",
})

// file database
db, err := orm.NewSQLite(orm.Config{
    Database: "/var/data/app.db",
})

// without Database an in-memory database is used automatically (one database per instance)
db, err := orm.NewSQLite(orm.Config{})
```

> **About in-memory databases**: when `Database` is not set, every `New`/`NewSQLite` call creates a
> memory database **exclusive to that instance** (the DSN looks like
> `file:orm_mem_<pid>_<seq>?mode=memory&cache=shared`).
>
> The two instances cannot see each other — and this matters: SQLite's `cache=shared` is keyed by DSN
> name and shared **across the whole process**, so if you use a fixed DSN name, tables created or data
> written by one component become visible to another, and everything is lost when the process exits
> (easily mistaken for "data mysteriously disappearing").
>
> Keeping `cache=shared` is nonetheless necessary: an anonymous `:memory:` would give **each connection**
> in the pool its own separate memory database, so after creating a table other connections can't see it —
> a far more common pitfall.

## Logging integration

The ORM package automatically bridges GORM's logs to the `logger` package. Just initialize the global logger at the program entry point and ORM's SQL logs are emitted through logger:

```go
package main

import (
    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/orm"
)

func main() {
    // initialize the global logger
    l := logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "myapp",
    })
    logger.SetGlobal(l)
    // The ILogger interface has no Close (Close is only on the concrete *Logger type);
    // flush the buffer with the package-level Sync before the global instance exits
    defer logger.Sync()

    // ORM logs are bridged to logger automatically
    db := orm.MustNewSQLite(orm.Config{
        Database: ":memory:",
        LogLevel: orm.LogInfo, // output SQL logs
    })

    // SQL logs go out through logger
    db.Exec("SELECT 1")
}
```

## Table name strategy

```go
db, err := orm.New(orm.Config{
    Driver:      orm.DriverSQLite,
    Database:    ":memory:",
    TablePrefix: "t_",      // table name prefix
    SingularTable: true,    // singular table names
})

db.AutoMigrate(&User{})
// the User model's table name is "t_user" (prefixed + singular)
```

## Connection pool

```go
db, err := orm.New(orm.Config{
    Driver:          orm.DriverMySQL,
    Host:            "127.0.0.1",
    Port:            3306,
    Username:        "root",
    Password:        "secret",
    Database:        "myapp",
    MaxIdleConns:    20,                  // max idle connections
    MaxOpenConns:    200,                 // max open connections
    ConnMaxLifetime: 1 * time.Hour,       // connection lifetime
    ConnMaxIdleTime: 30 * time.Minute,    // idle timeout
})
```

## Complete example

```go
package main

import (
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/orm"
)

type User struct {
    ID        uint      `gorm:"primaryKey"`
    Name      string    `gorm:"size:128;not null"`
    Email     string    `gorm:"size:256;uniqueIndex"`
    Age       int       `gorm:"default:0"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

func main() {
    // initialize logging
    logInstance := logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "demo",
    })
    logger.SetGlobal(logInstance)
    // The ILogger interface has no Close (Close is only on the concrete *Logger type);
    // flush the buffer with the package-level Sync before exiting
    defer logger.Sync()

    // create the database connection
    db := orm.MustNewSQLite(orm.Config{
        Database:  ":memory:",
        LogLevel:  orm.LogInfo,
        TablePrefix: "t_",
    })
    defer func() { _ = orm.Close(db) }()

    // automatic migration
    if err := db.AutoMigrate(&User{}); err != nil {
        logger.Fatal("failed to migrate database", logger.Err(err))
    }

    // create
    user := User{Name: "alice", Email: "alice@example.com", Age: 30}
    if err := db.Create(&user).Error; err != nil {
        logger.Fatal("failed to create user", logger.Err(err))
    }
    fmt.Printf("Created user: ID=%d\n", user.ID)

    // query
    var found User
    if err := db.First(&found, user.ID).Error; err != nil {
        logger.Fatal("failed to find user", logger.Err(err))
    }
    fmt.Printf("Found user: Name=%s, Email=%s\n", found.Name, found.Email)

    // update
    found.Age = 31
    if err := db.Save(&found).Error; err != nil {
        logger.Fatal("failed to update user", logger.Err(err))
    }

    // delete
    if err := db.Delete(&found).Error; err != nil {
        logger.Fatal("failed to delete user", logger.Err(err))
    }

    logger.Info("demo completed")
}
```

## Performance recommendations

- **SkipDefaultTransaction**: enabled by default; ordinary writes are not wrapped in a transaction, improving performance by roughly 30%
- **Connection pool**: in production keep `MaxOpenConns` below 80% of the database's `max_connections`
- **ConnMaxLifetime**: set it lower than the database's `wait_timeout` so connections aren't closed by the server
- **LogLevel**: use `LogWarn` or `LogError` in production, `LogInfo` in development to inspect SQL
- **SlowThreshold**: 200ms~500ms is recommended in production; adjust to your workload
