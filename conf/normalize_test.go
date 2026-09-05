package conf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 解析错误分支 ---

func TestLoad_ParseJSONError(t *testing.T) {
	file := createTempFile(t, ".json", `{invalid json`)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestLoad_ParseYAMLError(t *testing.T) {
	file := createTempFile(t, ".yaml", "a: [unclosed")
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestLoadFromJSONBytes_Invalid(t *testing.T) {
	var cfg TestConfig
	err := LoadFromJSONBytes([]byte(`{oops`), &cfg)
	assert.Error(t, err)
}

func TestLoadFromJSONBytes_TypeMismatch(t *testing.T) {
	// port 字段期望 int，但传入嵌套对象 → unmarshal 失败
	var cfg struct {
		Port int `json:"port"`
	}
	err := LoadFromJSONBytes([]byte(`{"port":{}}`), &cfg)
	assert.Error(t, err)
}

func TestLoadFromYAMLBytes_Invalid(t *testing.T) {
	var cfg TestConfig
	err := LoadFromYAMLBytes([]byte("a: [1,2"), &cfg)
	assert.Error(t, err)
}

func TestLoadFromYAMLBytes_TypeMismatch(t *testing.T) {
	var cfg struct {
		Port int `json:"port"`
	}
	err := LoadFromYAMLBytes([]byte("port: [x]"), &cfg)
	assert.Error(t, err)
}

// --- nil / 空值边界 ---

func TestLowercaseKeys_Nil(t *testing.T) {
	assert.Nil(t, lowercaseKeys(nil))
}

func TestLowercaseValues_Nil(t *testing.T) {
	assert.Nil(t, lowercaseValues(nil))
}

func TestNormalizeMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeMap(nil))
}

func TestLoadFromJSONBytes_Invalid2(t *testing.T) {
	_, err := loadFromJSONBytes([]byte("not json"))
	assert.Error(t, err)
}

func TestLoadFromYAMLBytes_Invalid2(t *testing.T) {
	_, err := loadFromYAMLBytes([]byte(": bad"))
	assert.Error(t, err)
}

// --- normalizeValue 各类型 ---

func TestNormalizeValue_NumericTypes(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int(1), "1"},
		{int8(8), "8"},
		{int16(16), "16"},
		{int32(32), "32"},
		{int64(64), "64"},
		{uint(7), "7"},
		{uint8(1), "1"},
		{uint16(2), "2"},
		{uint32(3), "3"},
		{uint64(4), "4"},
		{float32(1.5), "1.5"},
		{float64(2.5), "2.5"},
		{json.Number("9"), "9"},
	}
	for _, c := range cases {
		got := normalizeValue(c.in)
		assert.Equal(t, json.Number(c.want), got, "normalizeValue(%#v)", c.in)
	}
}

func TestNormalizeValue_Scalar(t *testing.T) {
	// bool / string 原样返回
	assert.Equal(t, true, normalizeValue(true))
	assert.Equal(t, "str", normalizeValue("str"))
	// nil 原样返回
	assert.Nil(t, normalizeValue(nil))
	// 默认分支：无法识别的类型 → fmt.Sprintf("%v")
	assert.Equal(t, "{y}", normalizeValue(struct{ x string }{"y"}))
}

func TestNormalizeValue_Maps(t *testing.T) {
	// map[string]any → normalizeMap
	got := normalizeValue(map[string]any{"k": int(1)})
	assert.Equal(t, map[string]any{"k": json.Number("1")}, got)

	// map[any]any → normalizeAnyKeyMap
	got = normalizeValue(map[any]any{1: "a"})
	assert.Equal(t, map[string]any{"1": "a"}, got)
}

func TestNormalizeValue_Slices(t *testing.T) {
	// []any → normalizeSlice
	got := normalizeValue([]any{int(1), "a", true})
	assert.Equal(t, []any{json.Number("1"), "a", true}, got)

	// []map[string]any
	got = normalizeValue([]map[string]any{{"k": int(1)}})
	assert.Equal(t, []any{map[string]any{"k": json.Number("1")}}, got)

	// []map[any]any
	got = normalizeValue([]map[any]any{{int(1): "a"}})
	assert.Equal(t, []any{map[string]any{"1": "a"}}, got)
}

func TestNormalizeAnyKeyMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeAnyKeyMap(nil))
}

func TestNormalizeSlice_Nil(t *testing.T) {
	assert.Nil(t, normalizeSlice(nil))
}

// --- 端到端：YAML 复合结构触发 normalizeValue 全链路 ---

func TestLoad_YAML_Composite(t *testing.T) {
	text := `
db:
  host: localhost
  port: 5432
nums:
  - 1
  - 2
rate: 1.5
enabled: true
`
	type Nested struct {
		DB struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"db"`
		Nums    []int   `json:"nums"`
		Rate    float64 `json:"rate"`
		Enabled bool    `json:"enabled"`
	}

	file := createTempFile(t, ".yaml", text)
	var cfg Nested
	err := Load(file, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
	assert.Equal(t, []int{1, 2}, cfg.Nums)
	assert.Equal(t, 1.5, cfg.Rate)
	assert.True(t, cfg.Enabled)
}

// YAML 整数 key 触发 map[any]any 规范化路径。
func TestLoad_YAML_IntKeys(t *testing.T) {
	text := `
m:
  1: a
  2: b
`
	var cfg struct {
		M map[string]string `json:"m"`
	}
	file := createTempFile(t, ".yaml", text)
	err := Load(file, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "a", cfg.M["1"])
	assert.Equal(t, "b", cfg.M["2"])
}

func TestLoad_YAML_SnakeCaseNested(t *testing.T) {
	text := `
server:
  port: 9090
name: app
`
	var cfg struct {
		Server struct {
			Port int `json:"port"`
		} `json:"server"`
		Name string `json:"name"`
	}
	file := createTempFile(t, ".yaml", text)
	err := Load(file, &cfg)
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "app", cfg.Name)
}

// mustJSONNumber 断言 v 是 json.Number 并返回。
func mustJSONNumber(t *testing.T, v any) json.Number {
	t.Helper()
	n, ok := v.(json.Number)
	assert.True(t, ok, "expected json.Number, got %T", v)
	return n
}

// --- 字符串内容辅助断言 ---

func TestUnmarshalMap_LowercasesKeys(t *testing.T) {
	var cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	err := unmarshalMap(map[string]any{"HOST": "x", "PORT": json.Number("1")}, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Host)
	assert.Equal(t, 1, cfg.Port)
}

func TestLoad_EnvExpansion_EmptyVar(t *testing.T) {
	// 未设置的环境变量展开为空串
	file := createTempFile(t, ".json", `{"host": "${NOT_SET_VAR}"}`)
	var cfg struct {
		Host string `json:"host"`
	}
	err := Load(file, &cfg, UseEnv())
	require.NoError(t, err)
	assert.True(t, strings.Contains(cfg.Host, ""))
}
