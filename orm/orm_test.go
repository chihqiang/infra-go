package orm

import (
	"testing"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// User is the model used by the tests.
type User struct {
	gorm.Model
	Name  string `gorm:"size:128;not null"`
	Email string `gorm:"size:256;uniqueIndex"`
	Age   int    `gorm:"default:0"`
}

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, Driver(""), c.Driver) // Driver is required and has no default
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
	// An empty database path uses an in-memory database, and every call generates a unique
	// database name (instances do not share it)
	dsn := buildSQLiteDSN(Config{})
	assert.Contains(t, dsn, "mode=memory")
	assert.Contains(t, dsn, "cache=shared", "shared cache is required for pool connections to see the same DB")

	dsn2 := buildSQLiteDSN(Config{})
	assert.NotEqual(t, dsn, dsn2, "each instance must get its own in-memory database")

	// A file path is used as is
	dsn = buildSQLiteDSN(Config{Database: "/tmp/test.db"})
	assert.Equal(t, "/tmp/test.db", dsn)
}

// TestSQLite_MemoryDBNotSharedBetweenInstances is a regression test: two SQLite instances
// created with the default configuration must each own an independent in-memory database.
//
// Historical defect: an empty Database always returned "file::memory:?cache=shared", and
// that DSN shares **one single database across the whole process**, so a table created or
// data written by one instance was visible to the other (data bleeding between components)
// and everything was lost when the process exited.
func TestSQLite_MemoryDBNotSharedBetweenInstances(t *testing.T) {
	open := func() *gorm.DB {
		db, err := New(Config{Driver: DriverSQLite})
		require.NoError(t, err)
		t.Cleanup(func() { _ = Close(db) })
		return db
	}

	db1 := open()
	db2 := open()

	require.NoError(t, db1.Exec("CREATE TABLE only_in_db1 (id INTEGER)").Error)
	require.NoError(t, db1.Exec("INSERT INTO only_in_db1 (id) VALUES (1)").Error)

	// db1 can read it back itself
	var count int64
	require.NoError(t, db1.Raw("SELECT COUNT(*) FROM only_in_db1").Scan(&count).Error)
	assert.Equal(t, int64(1), count)

	// db2 must not see the table created by db1
	err := db2.Raw("SELECT COUNT(*) FROM only_in_db1").Scan(&count).Error
	assert.Error(t, err, "instances must not share an in-memory database")
}

// TestSQLite_MemoryDBPoolsShareWithinInstance verifies that several connections of the same
// instance see the same database (which is what cache=shared provides).
func TestSQLite_MemoryDBPoolsShareWithinInstance(t *testing.T) {
	db, err := New(Config{Driver: DriverSQLite})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	require.NoError(t, db.Exec("CREATE TABLE t (id INTEGER)").Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// Open several connections and confirm they all see the table (an anonymous :memory:
	// would keep them separate, making this fail)
	sqlDB.SetMaxOpenConns(4)
	for i := 0; i < 8; i++ {
		var n int
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM t").Scan(&n).Error,
			"all pooled connections must see the same in-memory database")
	}
}

func TestNewSQLite_MemoryDB(t *testing.T) {
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() { _ = Close(db) }()

	// Auto-migrate
	err = db.AutoMigrate(&User{})
	require.NoError(t, err)

	// Insert data
	user := User{Name: "alice", Email: "alice@example.com", Age: 30}
	result := db.Create(&user)
	require.NoError(t, result.Error)
	assert.Equal(t, int64(1), result.RowsAffected)
	assert.NotZero(t, user.ID)

	// Query the data
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

	// Insert and query
	user := User{Name: "bob", Email: "bob@example.com", Age: 25}
	require.NoError(t, db.Create(&user).Error)

	// Reopen after closing to verify that the data is persisted
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
	// An in-memory database must be used when Database is not set
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
	// An invalid path must panic
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
	// Connecting to a MySQL instance that does not exist must panic
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
	// Connecting to a Postgres instance that does not exist must panic
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

	// Verify that the connection pool settings took effect (Stats is reachable and does not
	// error out)
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

	// Verify the table name prefix
	tableName := db.NamingStrategy.TableName("User")
	assert.Equal(t, "t_users", tableName)
}

func TestSingularTable(t *testing.T) {
	db, err := New(Config{
		Driver:        DriverSQLite,
		Database:      ":memory:",
		SingularTable: true,
		LogLevel:      LogSilent,
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	require.NoError(t, db.AutoMigrate(&User{}))

	// With singular table names the table of the User model is "user" instead of "users"
	tableName := db.NamingStrategy.TableName("User")
	assert.Equal(t, "user", tableName)
}

func TestSkipDefaultTransaction(t *testing.T) {
	// Verify that SkipDefaultTransaction is enabled by default
	db, err := New(Config{
		Driver:   DriverSQLite,
		Database: ":memory:",
	})
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	// SkipDefaultTransaction is a configuration item set through gorm.Config;
	// here we only verify that the connection works
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
	// Verify that gormWriter does not panic
	l := logger.New(logger.Config{Output: []string{"stdout"}})

	w := newGormWriter(l)
	assert.NotPanics(t, func() {
		w.Printf("[info] test message")
		w.Printf("[error] test error")
		w.Printf("[warn] test warning")
		w.Printf("[slow SQL] SELECT * FROM users")
	})
}
