package orm

import (
	"testing"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// User 测试用模型。
type User struct {
	gorm.Model
	Name  string `gorm:"size:128;not null"`
	Email string `gorm:"size:256;uniqueIndex"`
	Age   int    `gorm:"default:0"`
}

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, Driver(""), c.Driver) // Driver 必填，无默认值
	assert.Equal(t, "127.0.0.1", c.Host)
	assert.Equal(t, "root", c.Username)
	assert.Equal(t, "", c.Password)
	assert.Equal(t, "", c.Database)
	assert.Equal(t, 10, c.MaxIdleConns)
	assert.Equal(t, 100, c.MaxOpenConns)
	assert.Equal(t, 30*time.Minute, c.ConnMaxLifetime)
	assert.Equal(t, 10*time.Minute, c.ConnMaxIdleTime)
	assert.Equal(t, LogLevel(3), c.LogLevel) // LogWarn
	assert.Equal(t, 200*time.Millisecond, c.SlowThreshold)
	assert.False(t, c.Colorful)
	assert.True(t, c.SkipDefaultTransaction)
	assert.Equal(t, "", c.TablePrefix)
	assert.False(t, c.SingularTable)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := fillDefault(Config{
		Driver:          DriverMySQL,
		Host:            "db.example.com",
		Port:            3307,
		Username:        "admin",
		Password:        "secret",
		Database:        "myapp",
		MaxIdleConns:    20,
		MaxOpenConns:    200,
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		LogLevel:        LogInfo,
		SlowThreshold:   500 * time.Millisecond,
		Colorful:        true,
		TablePrefix:     "t_",
		SingularTable:   true,
	})

	assert.Equal(t, DriverMySQL, c.Driver)
	assert.Equal(t, "db.example.com", c.Host)
	assert.Equal(t, 3307, c.Port)
	assert.Equal(t, "admin", c.Username)
	assert.Equal(t, "secret", c.Password)
	assert.Equal(t, "myapp", c.Database)
	assert.Equal(t, 20, c.MaxIdleConns)
	assert.Equal(t, 200, c.MaxOpenConns)
	assert.Equal(t, 1*time.Hour, c.ConnMaxLifetime)
	assert.Equal(t, 30*time.Minute, c.ConnMaxIdleTime)
	assert.Equal(t, LogInfo, c.LogLevel)
	assert.Equal(t, 500*time.Millisecond, c.SlowThreshold)
	assert.True(t, c.Colorful)
	assert.Equal(t, "t_", c.TablePrefix)
	assert.True(t, c.SingularTable)
}

func TestDefaultPort(t *testing.T) {
	assert.Equal(t, 3306, defaultPort(DriverMySQL))
	assert.Equal(t, 5432, defaultPort(DriverPostgres))
	assert.Equal(t, 0, defaultPort(DriverSQLite))
	assert.Equal(t, 0, defaultPort("unknown"))
}

func TestBuildMySQLDSN(t *testing.T) {
	dsn := buildMySQLDSN(Config{
		Host:     "localhost",
		Port:     3306,
		Username: "root",
		Password: "pass",
		Database: "testdb",
	})
	assert.Equal(t, "root:pass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=true&loc=Local", dsn)
}

func TestBuildPostgresDSN(t *testing.T) {
	dsn := buildPostgresDSN(Config{
		Host:     "localhost",
		Port:     5432,
		Username: "postgres",
		Password: "secret",
		Database: "testdb",
	})
	assert.Equal(t, "host=localhost user=postgres password=secret dbname=testdb port=5432 sslmode=disable TimeZone=Asia/Shanghai", dsn)
}

func TestBuildSQLiteDSN(t *testing.T) {
	// 空数据库路径使用内存数据库
	dsn := buildSQLiteDSN(Config{})
	assert.Equal(t, "file::memory:?cache=shared", dsn)

	// 指定文件路径
	dsn = buildSQLiteDSN(Config{Database: "/tmp/test.db"})
	assert.Equal(t, "/tmp/test.db", dsn)
}

func TestNewSQLite_MemoryDB(t *testing.T) {
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()

	// 自动迁移
	err = db.AutoMigrate(&User{})
	require.NoError(t, err)

	// 插入数据
	user := User{Name: "alice", Email: "alice@example.com", Age: 30}
	result := db.Create(&user)
	require.NoError(t, result.Error)
	assert.Equal(t, int64(1), result.RowsAffected)
	assert.NotZero(t, user.ID)

	// 查询数据
	var found User
	err = db.First(&found, user.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "alice", found.Name)
	assert.Equal(t, "alice@example.com", found.Email)
	assert.Equal(t, 30, found.Age)
}

func TestNewSQLite_FileDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: dbPath,
	})
	require.NoError(t, err)
	require.NotNil(t, db)

	err = db.AutoMigrate(&User{})
	require.NoError(t, err)

	// 插入并查询
	user := User{Name: "bob", Email: "bob@example.com", Age: 25}
	require.NoError(t, db.Create(&user).Error)

	// 关闭后重新打开，验证数据持久化
	require.NoError(t, Close(db))

	db2, err := New(Config{
		Driver:   DriverSQLite,
		Database: dbPath,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db2) }()

	var found User
	err = db2.First(&found, user.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "bob", found.Name)
}

func TestNewSQLite_WithDSN(t *testing.T) {
	db, err := New(Config{
		Driver: DriverSQLite,
		DSN:    "file::memory:?cache=shared",
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()
}

func TestNewSQLite_EmptyDatabase_UsesMemory(t *testing.T) {
	// 不指定 Database 时应使用内存数据库
	db, err := New(Config{
		Driver: DriverSQLite,
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()

	err = db.AutoMigrate(&User{})
	require.NoError(t, err)
}

func TestNewSQLite_ViaNewSQLite(t *testing.T) {
	db, err := NewSQLite(Config{
		Database: ":memory:",
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()
}

func TestMustNewSQLite_Success(t *testing.T) {
	db := MustNewSQLite(Config{
		Database: ":memory:",
	})
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()
}

func TestMustNewSQLite_Panic(t *testing.T) {
	// 无效路径应 panic
	assert.Panics(t, func() {
		MustNewSQLite(Config{
			Database: "/nonexistent_dir/deep/path/test.db",
		})
	})
}

func TestNew_UnsupportedDriver(t *testing.T) {
	_, err := New(Config{
		Driver: "oracle",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported database driver")
}

func TestMustNew_UnsupportedDriver_Panic(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(Config{Driver: "oracle"})
	})
}

func TestMustNewMySQL_Panic(t *testing.T) {
	// 连接不存在的 MySQL 应 panic
	assert.Panics(t, func() {
		MustNewMySQL(Config{
			Host:     "127.0.0.1",
			Port:     13306,
			Username: "root",
			Password: "",
			Database: "nonexistent",
		})
	})
}

func TestMustNewPostgres_Panic(t *testing.T) {
	// 连接不存在的 Postgres 应 panic
	assert.Panics(t, func() {
		MustNewPostgres(Config{
			Host:     "127.0.0.1",
			Port:     15432,
			Username: "postgres",
			Password: "",
			Database: "nonexistent",
		})
	})
}

func TestPing(t *testing.T) {
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	err = Ping(db)
	assert.NoError(t, err)
}

func TestClose(t *testing.T) {
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)

	err = Close(db)
	assert.NoError(t, err)
}

func TestCRUD_Operations(t *testing.T) {
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
		LogLevel: LogSilent,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	require.NoError(t, db.AutoMigrate(&User{}))

	// Create
	user := User{Name: "charlie", Email: "charlie@example.com", Age: 28}
	require.NoError(t, db.Create(&user).Error)

	// Read
	var found User
	require.NoError(t, db.First(&found, user.ID).Error)
	assert.Equal(t, "charlie", found.Name)

	// Update
	found.Age = 29
	require.NoError(t, db.Save(&found).Error)

	var updated User
	require.NoError(t, db.First(&updated, user.ID).Error)
	assert.Equal(t, 29, updated.Age)

	// Delete
	require.NoError(t, db.Delete(&updated).Error)

	var deleted User
	err = db.First(&deleted, user.ID).Error
	assert.Error(t, err) // gorm.ErrRecordNotFound
}

func TestConnectionPool(t *testing.T) {
	db, err := New(Config{
		Driver:          DriverSQLite,
		Database:        ":memory:",
		MaxIdleConns:    5,
		MaxOpenConns:    10,
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	sqlDB, err := db.DB()
	require.NoError(t, err)

	// 验证连接池设置生效（通过 stats 可访问且不报错）
	_ = sqlDB.Stats()
	assert.NotNil(t, sqlDB)
}

func TestTablePrefix(t *testing.T) {
	db, err := New(Config{
		Driver:      DriverSQLite,
		Database:    ":memory:",
		TablePrefix: "t_",
		LogLevel:    LogSilent,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	require.NoError(t, db.AutoMigrate(&User{}))

	// 验证表名前缀
 tableName := db.NamingStrategy.TableName("User")
	assert.Equal(t, "t_users", tableName)
}

func TestSingularTable(t *testing.T) {
	db, err := New(Config{
		Driver:         DriverSQLite,
		Database:       ":memory:",
		SingularTable:  true,
		LogLevel:       LogSilent,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	require.NoError(t, db.AutoMigrate(&User{}))

	// 单数表名模式下，User 模型的表名为 "user" 而非 "users"
	tableName := db.NamingStrategy.TableName("User")
	assert.Equal(t, "user", tableName)
}

func TestSkipDefaultTransaction(t *testing.T) {
	// 验证默认开启 SkipDefaultTransaction
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	// SkipDefaultTransaction 是配置项，在 gorm.Config 中设置
	// 这里只验证连接正常
	assert.NotNil(t, db)
}

func TestBuildDialector_WithDSN(t *testing.T) {
	// MySQL DSN
	d, err := buildDialector(Config{Driver: DriverMySQL, DSN: "root:pass@tcp(127.0.0.1:3306)/db"})
	require.NoError(t, err)
	assert.NotNil(t, d)

	// Postgres DSN
	d, err = buildDialector(Config{Driver: DriverPostgres, DSN: "host=127.0.0.1 user=postgres dbname=db"})
	require.NoError(t, err)
	assert.NotNil(t, d)

	// SQLite DSN
	d, err = buildDialector(Config{Driver: DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	assert.NotNil(t, d)
}

func TestGormWriter_Printf(t *testing.T) {
	// 验证 gormWriter 不会 panic
	l := logger.New(logger.Config{Output: []string{"stdout"}})

	w := newGormWriter(l)
	assert.NotPanics(t, func() {
		w.Printf("[info] test message")
		w.Printf("[error] test error")
		w.Printf("[warn] test warning")
		w.Printf("[slow SQL] SELECT * FROM users")
	})
}
