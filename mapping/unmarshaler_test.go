package mapping

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UnmarshalJsonMap / Unmarshal 测试 ---

func TestUnmarshal_Basic(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}

	m := map[string]any{"name": "test", "port": json.Number("8080")}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_Default(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:"port"`
	}

	m := map[string]any{"port": json.Number("8080")}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_Optional(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
		Port int    `json:",optional"`
	}

	m := map[string]any{"name": "test"}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 0, cfg.Port)
}

func TestUnmarshal_RequiredMissing(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}

	m := map[string]any{}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_Options(t *testing.T) {
	type Config struct {
		Mode string `json:"mode,options=[file,console]"`
	}

	m := map[string]any{"mode": "file"}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "file", cfg.Mode)

	m2 := map[string]any{"mode": "invalid"}
	var cfg2 Config
	err = UnmarshalJsonMap(m2, &cfg2)
	assert.Error(t, err)
}

func TestUnmarshal_Range(t *testing.T) {
	type Config struct {
		Port int `json:"port,range=[1:65535]"`
	}

	m := map[string]any{"port": json.Number("8080")}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)

	m2 := map[string]any{"port": json.Number("0")}
	var cfg2 Config
	err = UnmarshalJsonMap(m2, &cfg2)
	assert.Error(t, err)
}

func TestUnmarshal_EnvVar(t *testing.T) {
	t.Setenv("TEST_ENV_VAR", "envvalue")
	type Config struct {
		Name string `json:",env=TEST_ENV_VAR"`
		Port int    `json:"port"`
	}

	m := map[string]any{"port": json.Number("8080")}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "envvalue", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_Duration(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:"timeout,default=5s"`
	}

	m := map[string]any{"timeout": "10s"}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
}

func TestUnmarshal_DurationDefault(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:"timeout,default=5s"`
	}

	m := map[string]any{}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
}

func TestUnmarshal_NestedStruct(t *testing.T) {
	type DB struct {
		Host string `json:",default=localhost"`
		Port int    `json:"port"`
	}
	type Config struct {
		DB DB `json:"db"`
	}

	m := map[string]any{
		"db": map[string]any{"port": json.Number("5432")},
	}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
}

func TestUnmarshal_Slice(t *testing.T) {
	type Config struct {
		Hosts []string `json:"hosts"`
		Ports []int    `json:"ports"`
	}

	m := map[string]any{
		"hosts": []any{"a.com", "b.com"},
		"ports": []any{json.Number("8080"), json.Number("9090")},
	}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a.com", "b.com"}, cfg.Hosts)
	assert.Equal(t, []int{8080, 9090}, cfg.Ports)
}

func TestUnmarshal_Map(t *testing.T) {
	type Config struct {
		Labels map[string]string `json:"labels"`
	}

	m := map[string]any{
		"labels": map[string]any{"env": "prod", "zone": "us-east-1"},
	}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "prod", cfg.Labels["env"])
	assert.Equal(t, "us-east-1", cfg.Labels["zone"])
}

func TestUnmarshal_Pointer(t *testing.T) {
	type Config struct {
		Host *string `json:"host"`
	}

	m := map[string]any{"host": "localhost"}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.NotNil(t, cfg.Host)
	assert.Equal(t, "localhost", *cfg.Host)
}

func TestUnmarshal_AnonymousField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
		Port int    `json:",default=8080"`
	}
	type Server struct {
		Base
		Name string `json:"name"`
	}

	m := map[string]any{"name": "api"}
	var cfg Server
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "api", cfg.Name)
}

func TestUnmarshal_NotPointer(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, cfg)
	assert.Error(t, err)
}

func TestUnmarshal_NotStruct(t *testing.T) {
	var i int
	err := UnmarshalJsonMap(map[string]any{}, &i)
	assert.Error(t, err)
}

func TestUnmarshal_LargeInt(t *testing.T) {
	type Config struct {
		ID int64 `json:"id"`
	}

	m := map[string]any{"id": json.Number("1234567890123456789")}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, int64(1234567890123456789), cfg.ID)
}

func TestUnmarshal_WithOptions(t *testing.T) {
	type Config struct {
		Host string `json:"host,default=0.0.0.0"`
	}

	u := NewUnmarshaler("json", WithDefault())
	var cfg Config
	err := u.Unmarshal(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Host)
}

func TestFillDefault(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	var cfg Config
	err := FillDefault(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
}

// --- 补充：选项与未覆盖分支 ---

func TestUnmarshal_WithStringValues(t *testing.T) {
	type Config struct {
		Port int     `json:"port"`
		Rate float64 `json:"rate"`
		On   bool    `json:"on"`
	}

	m := map[string]any{"port": 8080, "rate": 1.5, "on": true}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg, WithStringValues())
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 1.5, cfg.Rate)
	assert.True(t, cfg.On)
}

func TestUnmarshal_WithCanonicalKeyFunc(t *testing.T) {
	type Config struct {
		LogMode string `json:"logMode"`
	}

	m := map[string]any{"logmode": "console"} // 键为小写
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg, WithCanonicalKeyFunc(strings.ToLower))
	assert.NoError(t, err)
	assert.Equal(t, "console", cfg.LogMode)
}

func TestUnmarshalKey(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}

	m := map[string]any{"name": "test", "port": json.Number("8080")}
	var cfg Config
	err := UnmarshalKey(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_PointerToPointer(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
	}

	var p *Config
	err := UnmarshalJsonMap(map[string]any{"name": "x"}, &p)
	assert.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "x", p.Name)
}

func TestUnmarshal_NativeIntRange(t *testing.T) {
	type Config struct {
		Port int `json:"port,range=[1:8080]"`
	}

	// 原生 int 值越界（非 json.Number）→ validateValueRange 报错
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": 9090}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_ConvertedValue(t *testing.T) {
	type Config struct {
		Port int `json:"port"`
	}

	// map 值为 int64（与 int 字段类型不同）→ 走 setConvertedValue 字符串转换成功
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": int64(8080)}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_ConvertedValue_FloatToIntError(t *testing.T) {
	type Config struct {
		Port int `json:"port"`
	}

	// 小数无法转 int → 报错
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": 3.14}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_StringToInt_ConvertError(t *testing.T) {
	type Config struct {
		Port int `json:"port"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": "abc"}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_StructNotMap(t *testing.T) {
	type DB struct {
		Port int `json:"port"`
	}
	type Config struct {
		DB DB `json:"db"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"db": "notmap"}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_NilValue(t *testing.T) {
	type Config struct {
		Name string `json:",optional"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"name": nil}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Name)
}

func TestUnmarshal_NilValueRequired(t *testing.T) {
	type Config struct {
		Name string `json:"name"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"name": nil}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_SliceOfStruct(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	type Config struct {
		Items []Item `json:"items"`
	}

	m := map[string]any{"items": []any{
		map[string]any{"id": json.Number("1"), "name": "a"},
		map[string]any{"id": json.Number("2"), "name": "b"},
	}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Len(t, cfg.Items, 2)
	assert.Equal(t, "a", cfg.Items[0].Name)
	assert.Equal(t, 2, cfg.Items[1].ID)
}

func TestUnmarshal_SliceOfDuration(t *testing.T) {
	type Config struct {
		Timeouts []time.Duration `json:"timeouts"`
	}

	// duration 元素以纳秒数值形式提供
	m := map[string]any{"timeouts": []any{json.Number("1000000000"), json.Number("2000000000")}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, cfg.Timeouts)
}

func TestUnmarshal_SliceOfSlice(t *testing.T) {
	type Config struct {
		Matrix [][]string `json:"matrix"`
	}

	m := map[string]any{"matrix": []any{[]any{"a", "b"}, []any{"c"}}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, cfg.Matrix)
}

func TestUnmarshal_SliceNotSlice(t *testing.T) {
	type Config struct {
		Hosts []string `json:"hosts"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"hosts": "a"}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_SliceOfBool(t *testing.T) {
	type Config struct {
		Flags []bool `json:"flags"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"flags": []any{true, false}}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []bool{true, false}, cfg.Flags)
}

func TestUnmarshal_SliceOfStringFromInt(t *testing.T) {
	type Config struct {
		Codes []string `json:"codes"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"codes": []any{1, 2}}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, cfg.Codes)
}

func TestUnmarshal_MapSameType(t *testing.T) {
	type Config struct {
		Labels map[string]string `json:"labels"`
	}
	// map 已是同类型 → 直接 Set
	existing := map[string]string{"a": "1"}
	var cfg Config
	cfg.Labels = existing
	err := UnmarshalJsonMap(map[string]any{"labels": map[string]string{"a": "1"}}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "1", cfg.Labels["a"])
}

func TestUnmarshal_MapOfStruct(t *testing.T) {
	type Svc struct {
		Addr string `json:"addr"`
	}
	type Config struct {
		Svcs map[string]Svc `json:"svcs"`
	}

	m := map[string]any{"svcs": map[string]any{
		"api": map[string]any{"addr": "x"},
	}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "x", cfg.Svcs["api"].Addr)
}

func TestUnmarshal_MapOfDuration(t *testing.T) {
	type Config struct {
		Timeouts map[string]time.Duration `json:"timeouts"`
	}

	// duration 值以纳秒数值形式提供
	m := map[string]any{"timeouts": map[string]any{"a": json.Number("1000000000")}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, time.Second, cfg.Timeouts["a"])
}

func TestUnmarshal_MapOfSlice(t *testing.T) {
	type Config struct {
		M map[string][]int `json:"m"`
	}

	m := map[string]any{"m": map[string]any{"k": []any{json.Number("1"), json.Number("2")}}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2}, cfg.M["k"])
}

func TestUnmarshal_MapOfMap(t *testing.T) {
	type Config struct {
		M map[string]map[string]int `json:"m"`
	}

	m := map[string]any{"m": map[string]any{
		"outer": map[string]any{"inner": json.Number("1")},
	}}
	var cfg Config
	err := UnmarshalJsonMap(m, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, cfg.M["outer"]["inner"])
}

func TestUnmarshal_MapOfInt(t *testing.T) {
	type Config struct {
		Counts map[string]int `json:"counts"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"counts": map[string]any{"a": json.Number("5")}}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 5, cfg.Counts["a"])
}

func TestUnmarshal_MapKeyMismatch(t *testing.T) {
	type Config struct {
		M map[string]int `json:"m"`
	}
	// 键类型不匹配（int key）
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"m": map[int]any{1: json.Number("5")}}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_DefaultSliceJSON(t *testing.T) {
	type Config struct {
		Tags []string `json:"tags,default=[a,b]"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, cfg.Tags)
}

func TestUnmarshal_DefaultSliceComma(t *testing.T) {
	// 裸逗号需在标签中转义为 \,（否则标签解析阶段即被切分）
	type Config struct {
		Tags []string `json:"tags,default=a\\,b"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, cfg.Tags)
}

func TestUnmarshal_MissingDuration_Required(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:"timeout"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_MissingDuration_Optional(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:",optional"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
}

func TestUnmarshal_Duration_Invalid(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:"timeout"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"timeout": "abc"}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_MissingNestedStruct_Required(t *testing.T) {
	type DB struct {
		Port int `json:"port"`
	}
	type Config struct {
		DB DB `json:"db"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_MissingNestedStruct_AllDefault(t *testing.T) {
	type DB struct {
		Host string `json:",default=localhost"`
	}
	type Config struct {
		DB DB `json:"db"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.DB.Host)
}

func TestUnmarshal_MissingSliceMap(t *testing.T) {
	type Config struct {
		Hosts  []string          `json:"hosts"`
		Labels map[string]string `json:"labels"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
}

func TestUnmarshal_EnvIntField(t *testing.T) {
	t.Setenv("TEST_MAPPING_INT", "42")
	type Config struct {
		Port int `json:",env=TEST_MAPPING_INT"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 42, cfg.Port)
}

func TestUnmarshal_EnvDurationField(t *testing.T) {
	t.Setenv("TEST_MAPPING_DUR", "5s")
	type Config struct {
		Timeout time.Duration `json:",env=TEST_MAPPING_DUR"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
}

func TestUnmarshal_EnvNotInOptions(t *testing.T) {
	t.Setenv("TEST_MAPPING_MODE", "invalid")
	type Config struct {
		Mode string `json:",env=TEST_MAPPING_MODE,options=[a,b]"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_UsingDifferentTagKey(t *testing.T) {
	// 字段只用 form 标签，无 json 标签 → json 解析时应跳过
	type Config struct {
		Name string `form:"name"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"name": "x"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Name) // 未设置
}

func TestUnmarshal_IgnoreDashField(t *testing.T) {
	type Config struct {
		Name string `json:"-"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"Name": "x"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Name)
}

func TestUnmarshal_OptionalEmbeddedNoValue(t *testing.T) {
	type Base struct {
		Host string `json:"host"`
	}
	type Config struct {
		Base `json:",optional"`
		Name string `json:"name"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"name": "api"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "api", cfg.Name)
}

func TestFillDefault_NonZeroFieldError(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
	}
	cfg := Config{Host: "already"}
	err := FillDefault(&cfg)
	assert.Error(t, err)
}

func TestFillDefault_NestedEnv(t *testing.T) {
	t.Setenv("TEST_MAPPING_DB", "envhost")
	type DB struct {
		Host string `json:",default=localhost,env=TEST_MAPPING_DB"`
	}
	type Config struct {
		DB DB `json:"db"`
	}
	var cfg Config
	err := FillDefault(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, "envhost", cfg.DB.Host)
}

// --- 补充：tag 中 bool/uint/float 默认值经 setMatchedPrimitiveValue ---

func TestUnmarshal_DefaultBool(t *testing.T) {
	type Config struct {
		On bool `json:",default=true"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.True(t, cfg.On)
}

func TestUnmarshal_DefaultFloat(t *testing.T) {
	type Config struct {
		Ratio float64 `json:",default=0.75"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.InDelta(t, 0.75, cfg.Ratio, 0.001)
}

func TestUnmarshal_DefaultUint(t *testing.T) {
	type Config struct {
		Count uint `json:",default=10"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, uint(10), cfg.Count)
}

func TestUnmarshal_NamedIntType(t *testing.T) {
	// 自定义类型需 Convert
	type Port int32
	type Config struct {
		Port Port `json:"port"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": json.Number("8080")}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, Port(8080), cfg.Port)
}

func TestDeref_MultiPointer(t *testing.T) {
	type T struct{}
	var p **T
	assert.Equal(t, reflect.TypeOf(T{}), Deref(reflect.TypeOf(p)))
}

func TestUnmarshal_UnsupportedTypeDefault(t *testing.T) {
	// default 值无法转成目标类型
	type Config struct {
		Port int `json:",default=abc"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_NativeStringToIntField(t *testing.T) {
	// map 中 string 值赋给 int 字段 → setStringValue 转换
	type Config struct {
		Port int `json:"port"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": "8080"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}
