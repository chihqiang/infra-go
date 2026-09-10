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

// --- 回归：nil / 类型化 nil / 匿名嵌入指针（此前均 panic）---

// TestFillAndOverride_NilOverrides 验证 overrides 为 nil 时仅填充默认值，不 panic。
// 历史缺陷：reflect.ValueOf(nil) 得到无效值，取 Type() 时
// panic "reflect: call of reflect.Value.Type on zero Value"。
func TestFillAndOverride_NilOverrides(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	var c Config
	require.NotPanics(t, func() {
		require.NoError(t, FillAndOverride(&c, nil))
	})
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

// TestFillAndOverride_TypedNilOverrides 验证类型化 nil 指针同样视为"无覆盖"。
func TestFillAndOverride_TypedNilOverrides(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	var nilCfg *Config
	var c Config
	require.NotPanics(t, func() {
		require.NoError(t, FillAndOverride(&c, nilCfg))
	})
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

// TestFillAndOverride_AnonymousPtrField 验证匿名嵌入指针结构体可被递归覆盖，
// 且默认值得以保留。
// 历史缺陷：递归时未解引用，在 Ptr 值上调用 Field(i) 会
// panic "reflect: call of reflect.Value.Field on ptr Value"。
func TestFillAndOverride_AnonymousPtrField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
		Port int    `json:",default=80"`
	}
	type Server struct {
		*Base
		Name string `json:",default=svc"`
	}

	t.Run("target pointer nil, override provided", func(t *testing.T) {
		var c Server
		require.NotPanics(t, func() {
			require.NoError(t, FillAndOverride(&c, Server{
				Base: &Base{Port: 8080},
				Name: "api",
			}))
		})
		require.NotNil(t, c.Base, "embedded pointer should be allocated to receive overrides")
		assert.Equal(t, "0.0.0.0", c.Host, "default must be preserved")
		assert.Equal(t, 8080, c.Port)
		assert.Equal(t, "api", c.Name)
	})

	t.Run("override pointer nil keeps defaults", func(t *testing.T) {
		var c Server
		require.NotPanics(t, func() {
			require.NoError(t, FillAndOverride(&c, Server{Name: "api"}))
		})
		// 默认值来自 FillDefault（嵌入指针由 mapping 分配），override 为 nil 时不覆盖
		assert.Equal(t, "api", c.Name)
		if c.Base != nil {
			assert.Equal(t, "0.0.0.0", c.Host)
		}
	})
}

// TestFillAndOverride_AnonymousPtrFieldNestedNonZero 验证匿名嵌入指针的
// 非零子字段覆盖与默认值保留同时成立。
func TestFillAndOverride_AnonymousPtrFieldNestedNonZero(t *testing.T) {
	type Base struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=3306"`
	}
	type Config struct {
		*Base
		Name string `json:",default=app"`
	}

	var c Config
	require.NoError(t, FillAndOverride(&c, Config{Base: &Base{Host: "db.example.com"}}))
	require.NotNil(t, c.Base)
	assert.Equal(t, "db.example.com", c.Host)
	assert.Equal(t, 3306, c.Port, "unset sub-field must keep its default")
	assert.Equal(t, "app", c.Name)
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

// --- 补充：覆盖语义分支 ---

func TestFillAndOverride_OverridesPointer(t *testing.T) {
	type Config struct {
		Host string `json:",default=0.0.0.0"`
	}
	var c Config
	// overrides 传指针也应正常解引用
	err := FillAndOverride(&c, &Config{Host: "1.1.1.1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", c.Host)
}

func TestFillAndOverride_BoolField(t *testing.T) {
	type Config struct {
		Verbose bool `json:",optional"`
	}
	var c Config
	// true 覆盖
	err := FillAndOverride(&c, Config{Verbose: true})
	assert.NoError(t, err)
	assert.True(t, c.Verbose)

	// false 视为未设置 → 保留零值
	var c2 Config
	err = FillAndOverride(&c2, Config{Verbose: false})
	assert.NoError(t, err)
	assert.False(t, c2.Verbose)
}

func TestFillAndOverride_BoolPointerExplicitFalse(t *testing.T) {
	type Config struct {
		Verbose *bool `json:",optional"`
	}
	f := false
	var c Config
	err := FillAndOverride(&c, Config{Verbose: &f})
	assert.NoError(t, err)
	require.NotNil(t, c.Verbose)
	assert.False(t, *c.Verbose)
}

func TestFillAndOverride_SliceField(t *testing.T) {
	type Config struct {
		Tags []string `json:",optional"`
	}
	var c Config
	// 非空 slice 覆盖
	err := FillAndOverride(&c, Config{Tags: []string{"a"}})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a"}, c.Tags)

	// 空 slice 视为未设置 → 不覆盖（保持 nil）
	var c2 Config
	err = FillAndOverride(&c2, Config{Tags: []string{}})
	assert.NoError(t, err)
	assert.Nil(t, c2.Tags)
}

func TestFillAndOverride_MapField(t *testing.T) {
	type Config struct {
		Labels map[string]string `json:",optional"`
	}
	var c Config
	// 非空 map 覆盖
	err := FillAndOverride(&c, Config{Labels: map[string]string{"a": "1"}})
	assert.NoError(t, err)
	assert.Equal(t, "1", c.Labels["a"])
}

func TestFillAndOverride_UnexportedField(t *testing.T) {
	type Config struct {
		Host   string `json:",default=0.0.0.0"`
		hidden string // 未导出字段应被覆盖逻辑跳过
	}
	var c Config
	c.hidden = "keep-me" // 设置初始值：若误处理未导出字段，该值会被 src 覆盖
	err := FillAndOverride(&c, Config{hidden: "drop-me"})
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", c.Host)   // 导出字段仍按 default 填充
	assert.Equal(t, "keep-me", c.hidden) // 未导出字段保持原值，未被覆盖
}
