package mapping

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FillAndOverride 测试 ---

func TestFillAndOverride_Basic(t *testing.T) {
	type Config struct {
		Host    string        `json:",default=0.0.0.0"`
		Port    int           `json:",default=8080"`
		Timeout time.Duration `json:",default=5s"`
	}

	var c Config
	err := FillAndOverride(&c, Config{
		Host:    "127.0.0.1",
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", c.Host) // 非零覆盖
	assert.Equal(t, 8080, c.Port)        // 零值保留默认
	assert.Equal(t, 10*time.Second, c.Timeout)
}

func TestFillAndOverride_ZeroValueKeepsDefault(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	// overrides 全零值：应保留默认
	var c Config
	err := FillAndOverride(&c, Config{})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

func TestFillAndOverride_StringEmptyKeepsDefault(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
	}

	// string 空值视为未设置，不覆盖默认
	var c Config
	err := FillAndOverride(&c, Config{Host: ""})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.Host)
}

func TestFillAndOverride_OptionalStringAlwaysOverrides(t *testing.T) {
	type Config struct {
		Secret string `json:",optional"`
	}

	// optional 且无 default 的 string：空字符串也视为有效值（始终覆盖）
	var c Config
	err := FillAndOverride(&c, Config{Secret: "s3cret"})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", c.Secret)
}

func TestFillAndOverride_PointerExplicitZero(t *testing.T) {
	type Config struct {
		Port *int `json:",optional"`
	}

	zero := 0
	var c Config
	err := FillAndOverride(&c, Config{Port: &zero})
	require.NoError(t, err)
	require.NotNil(t, c.Port)
	assert.Equal(t, 0, *c.Port)
}

func TestFillAndOverride_NestedStruct(t *testing.T) {
	type DB struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=3306"`
	}
	type Config struct {
		DB DB `json:"db"`
	}

	// 嵌套结构体仅覆盖非零子字段
	var c Config
	err := FillAndOverride(&c, Config{DB: DB{Port: 5432}})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.DB.Host)
	assert.Equal(t, 5432, c.DB.Port)
}

func TestFillAndOverride_AnonymousField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
	}
	type Server struct {
		Base
		Name string `json:",default=svc"`
	}

	var c Server
	err := FillAndOverride(&c, Server{Name: "api"})
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", c.Host)
	assert.Equal(t, "api", c.Name)
}

func TestFillAndOverride_NotPointer(t *testing.T) {
	type Config struct {
		Name string `json:",default=x"`
	}

	var c Config
	err := FillAndOverride(c, Config{})
	assert.Error(t, err)
}

func TestFillAndOverride_TypeMismatch(t *testing.T) {
	type A struct {
		Name string
	}
	type B struct {
		Name string
	}

	var a A
	err := FillAndOverride(&a, B{})
	assert.Error(t, err)
}

func TestMustFillAndOverride(t *testing.T) {
	type Config struct {
		Host string `json:",default=0.0.0.0"`
	}

	var c Config
	MustFillAndOverride(&c, Config{Host: "1.2.3.4"})
	assert.Equal(t, "1.2.3.4", c.Host)
}

func TestMustFillAndOverride_Panic(t *testing.T) {
	type Config struct {
		Name string `json:",default=x"`
	}

	var c Config
	assert.Panics(t, func() {
		MustFillAndOverride(c, Config{})
	})
}
