# orm

基于 [GORM](https://gorm.io) 的数据库 ORM 包，支持 MySQL、PostgreSQL、SQLite 三种数据库，提供统一的配置结构体和连接管理。

## 特性

- **多数据库支持**：MySQL、PostgreSQL、SQLite，统一 API
- **配置驱动**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **DSN / 分字段两种连接方式**：可直接传 DSN，也可用 Host/Port/Username/Password 分字段配置
- **连接池管理**：可配置最大空闲连接、最大打开连接、连接存活时间
- **日志桥接**：自动将 GORM 日志桥接到 `logger` 包，无需额外配置
- **表名策略**：支持表名前缀、单数表名
- **便捷函数**：`MustNew` 系列 panic on error，`Ping` / `Close` 辅助函数
- **慢查询阈值**：可配置慢查询阈值，超时 SQL 自动标记

## 安装

```bash
go get github.com/chihqiang/infra-go/orm
```

## 快速开始

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
    // SQLite 内存数据库（适合开发和测试）
    db := orm.MustNewSQLite(orm.Config{
        Database: ":memory:",
    })

    // 自动迁移
    db.AutoMigrate(&User{})

    // 增删改查
    db.Create(&User{Name: "alice", Email: "alice@example.com"})

    var user User
    db.First(&user, 1)
}
```

## 配置

### Config 结构体

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

### 配置项说明

| 字段 | 类型 | 默认值 | 说明 |
| ------ | ------ | -------- | ------ |
| `Driver` | `Driver` | — | 数据库驱动：`mysql`、`postgres`、`sqlite`，必填 |
| `DSN` | `string` | `""` | 数据源名称，设置后优先使用，忽略 Host/Port 等字段 |
| `Host` | `string` | `127.0.0.1` | 数据库主机地址 |
| `Port` | `int` | 驱动默认端口 | MySQL 3306，Postgres 5432，SQLite 0 |
| `Username` | `string` | `root` | 数据库用户名 |
| `Password` | `string` | `""` | 数据库密码 |
| `Database` | `string` | `""` | 数据库名称（SQLite 为文件路径） |
| `MaxIdleConns` | `int` | `10` | 最大空闲连接数 |
| `MaxOpenConns` | `int` | `100` | 最大打开连接数 |
| `ConnMaxLifetime` | `Duration` | `30m` | 连接最大存活时间 |
| `ConnMaxIdleTime` | `Duration` | `10m` | 连接最大空闲时间 |
| `LogLevel` | `LogLevel` | `LogWarn` | GORM 日志级别 |
| `SlowThreshold` | `Duration` | `200ms` | 慢查询阈值 |
| `Colorful` | `bool` | `false` | 是否启用彩色日志输出 |
| `SkipDefaultTransaction` | `bool` | `true` | 跳过默认事务（提升约 30% 写入性能） |
| `TablePrefix` | `string` | `""` | 表名前缀 |
| `SingularTable` | `bool` | `false` | 是否使用单数表名 |

### 日志级别

```go
orm.LogSilent // 静默，不输出日志
orm.LogError  // 仅错误
orm.LogWarn   // 警告及以上（默认）
orm.LogInfo   // 全部日志（包括 SQL）
```

## API

### 通用连接函数

```go
// New 根据 Config.Driver 自动选择驱动
db, err := orm.New(cfg)

// MustNew 出错时 panic
db := orm.MustNew(cfg)
```

### 指定驱动连接函数

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

### 辅助函数

```go
// 测试连接
err := orm.Ping(db)

// 关闭连接
err := orm.Close(db)
```

## 各数据库示例

### MySQL

```go
// 方式一：分字段配置
db, err := orm.NewMySQL(orm.Config{
    Host:     "127.0.0.1",
    Port:     3306,
    Username: "root",
    Password: "secret",
    Database: "myapp",
})

// 方式二：DSN
db, err := orm.NewMySQL(orm.Config{
    DSN: "root:secret@tcp(127.0.0.1:3306)/myapp?charset=utf8mb4&parseTime=true",
})
```

### PostgreSQL

```go
// 方式一：分字段配置
db, err := orm.NewPostgres(orm.Config{
    Host:     "127.0.0.1",
    Port:     5432,
    Username: "postgres",
    Password: "secret",
    Database: "myapp",
})

// 方式二：DSN
db, err := orm.NewPostgres(orm.Config{
    DSN: "host=127.0.0.1 user=postgres password=secret dbname=mydb port=5432 sslmode=disable",
})
```

### SQLite

```go
// 内存数据库（适合测试）
db, err := orm.NewSQLite(orm.Config{
    Database: ":memory:",
})

// 文件数据库
db, err := orm.NewSQLite(orm.Config{
    Database: "/var/data/app.db",
})

// 不指定 Database 时自动使用内存数据库
db, err := orm.NewSQLite(orm.Config{})
```

## 日志集成

ORM 包自动将 GORM 的日志桥接到 `logger` 包。只需在程序入口初始化全局 logger，ORM 的 SQL 日志就会通过 logger 输出：

```go
package main

import (
    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/orm"
)

func main() {
    // 初始化全局 logger
    l := logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "myapp",
    })
    logger.SetGlobal(l)
    defer l.Close()

    // ORM 日志自动桥接到 logger
    db := orm.MustNewSQLite(orm.Config{
        Database: ":memory:",
        LogLevel: orm.LogInfo, // 输出 SQL 日志
    })

    // SQL 日志会通过 logger 输出
    db.Exec("SELECT 1")
}
```

## 表名策略

```go
db, err := orm.New(orm.Config{
    Driver:      orm.DriverSQLite,
    Database:    ":memory:",
    TablePrefix: "t_",      // 表名前缀
    SingularTable: true,    // 单数表名
})

db.AutoMigrate(&User{})
// User 模型的表名为 "t_user"（带前缀 + 单数）
```

## 连接池

```go
db, err := orm.New(orm.Config{
    Driver:          orm.DriverMySQL,
    Host:            "127.0.0.1",
    Port:            3306,
    Username:        "root",
    Password:        "secret",
    Database:        "myapp",
    MaxIdleConns:    20,                  // 最大空闲连接
    MaxOpenConns:    200,                 // 最大打开连接
    ConnMaxLifetime: 1 * time.Hour,       // 连接存活时间
    ConnMaxIdleTime: 30 * time.Minute,    // 空闲超时
})
```

## 完整示例

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
    // 初始化日志
    logInstance := logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "demo",
    })
    logger.SetGlobal(logInstance)
    defer logInstance.Close()

    // 创建数据库连接
    db := orm.MustNewSQLite(orm.Config{
        Database:  ":memory:",
        LogLevel:  orm.LogInfo,
        TablePrefix: "t_",
    })
    defer func() { _ = orm.Close(db) }()

    // 自动迁移
    if err := db.AutoMigrate(&User{}); err != nil {
        logger.FatalIf(err, "failed to migrate database")
    }

    // 创建
    user := User{Name: "alice", Email: "alice@example.com", Age: 30}
    if err := db.Create(&user).Error; err != nil {
        logger.FatalIf(err, "failed to create user")
    }
    fmt.Printf("Created user: ID=%d\n", user.ID)

    // 查询
    var found User
    if err := db.First(&found, user.ID).Error; err != nil {
        logger.FatalIf(err, "failed to find user")
    }
    fmt.Printf("Found user: Name=%s, Email=%s\n", found.Name, found.Email)

    // 更新
    found.Age = 31
    if err := db.Save(&found).Error; err != nil {
        logger.FatalIf(err, "failed to update user")
    }

    // 删除
    if err := db.Delete(&found).Error; err != nil {
        logger.FatalIf(err, "failed to delete user")
    }

    logger.Info("demo completed")
}
```

## 性能建议

- **SkipDefaultTransaction**：默认开启，普通写入不包裹事务，提升约 30% 性能
- **连接池**：生产环境建议 `MaxOpenConns` 不超过数据库 `max_connections` 的 80%
- **ConnMaxLifetime**：设置小于数据库 `wait_timeout` 的值，避免连接被服务端关闭
- **LogLevel**：生产环境使用 `LogWarn` 或 `LogError`，开发环境用 `LogInfo` 查看 SQL
- **SlowThreshold**：生产环境建议 200ms~500ms，根据业务调整
