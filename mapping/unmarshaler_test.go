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

// --- UnmarshalJsonMap / Unmarshal tests ---

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

// --- Additional coverage: options and uncovered branches ---

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

	m := map[string]any{"logmode": "console"} // the key is lower case
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

	// A native int value out of range (not a json.Number) -> validateValueRange errors
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": 9090}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_ConvertedValue(t *testing.T) {
	type Config struct {
		Port int `json:"port"`
	}

	// The map value is an int64 (a type different from the int field) -> the string
	// conversion in setConvertedValue succeeds
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": int64(8080)}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestUnmarshal_ConvertedValue_FloatToIntError(t *testing.T) {
	type Config struct {
		Port int `json:"port"`
	}

	// A fraction cannot be converted to int -> error
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

	// Duration elements are provided as nanosecond numbers
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
	// The map already has the same type -> set directly
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

	// The duration value is provided as a nanosecond number
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
	// Key type mismatch (int key)
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
	// A bare comma must be escaped as \, in the tag (otherwise the tag parser
	// splits on it)
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
	// The field only has a form tag and no json tag -> json parsing must skip it
	type Config struct {
		Name string `form:"name"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"name": "x"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Name) // not set
}

// --- range validation consistency (every write path must enforce it, no bypass
// by value source or representation) ---

// TestUnmarshal_RangeNotBypassedByStringValue verifies that range validation is
// not bypassed when the value type differs from the field type (e.g. port:
// "9090" in YAML).
// Historic defect: such values went through setConvertedValue with no range
// validation at all.
func TestUnmarshal_RangeNotBypassedByStringValue(t *testing.T) {
	type Config struct {
		Port int `json:"port,range=[1:8080]"`
	}

	// The same semantic value: a native int out of range errors, so the string form
	// must error as well
	var native Config
	err := UnmarshalJsonMap(map[string]any{"port": 9090}, &native)
	require.Error(t, err, "native int out of range must be rejected")

	var asString Config
	err = UnmarshalJsonMap(map[string]any{"port": "9090"}, &asString)
	require.Error(t, err, "string value out of range must be rejected too")

	// A string value within bounds still parses normally
	var ok Config
	err = UnmarshalJsonMap(map[string]any{"port": "8080"}, &ok)
	require.NoError(t, err)
	assert.Equal(t, 8080, ok.Port)
}

// TestUnmarshal_RangeNotBypassedByEnv verifies that range still applies when an
// environment variable overrides the config.
// Historic defect: setEnvValue validated options only, not range.
func TestUnmarshal_RangeNotBypassedByEnv(t *testing.T) {
	type Config struct {
		Port int `json:",range=[1:8080],env=TEST_RANGE_BYPASS_PORT"`
	}

	t.Setenv("TEST_RANGE_BYPASS_PORT", "99999")
	var bad Config
	err := UnmarshalJsonMap(map[string]any{}, &bad)
	require.Error(t, err, "env value out of range must be rejected")

	t.Setenv("TEST_RANGE_BYPASS_PORT", "8080")
	var ok Config
	err = UnmarshalJsonMap(map[string]any{}, &ok)
	require.NoError(t, err)
	assert.Equal(t, 8080, ok.Port)
}

// TestUnmarshal_RangeOnDuration verifies that the range constraint applies to
// duration fields.
// Historic defect: the duration branch skipped validation entirely; the range is a
// plain number (nanoseconds), matching time.Duration's underlying int64
// representation.
func TestUnmarshal_RangeOnDuration(t *testing.T) {
	type Config struct {
		// 1ns <= timeout <= 10s (10s = 10000000000ns)
		Timeout time.Duration `json:"timeout,range=[1:10000000000]"`
	}

	var ok Config
	err := UnmarshalJsonMap(map[string]any{"timeout": "5s"}, &ok)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, ok.Timeout)

	var bad Config
	err = UnmarshalJsonMap(map[string]any{"timeout": "60s"}, &bad)
	require.Error(t, err, "duration out of range must be rejected")
}

// TestUnmarshal_RangeOnDefaultValue verifies that a default declared in the tag
// must satisfy range as well.
// Historic defect: setDefaultValue performed no range validation, so an
// out-of-range default was written silently.
func TestUnmarshal_RangeOnDefaultValue(t *testing.T) {
	// Out-of-range default: a tag declaration error that must surface at load time
	type BadConfig struct {
		Port int `json:",default=99999,range=[1:8080]"`
	}
	var bad BadConfig
	err := UnmarshalJsonMap(map[string]any{}, &bad)
	require.Error(t, err, "out-of-range default must be rejected")

	// A valid default is still filled normally
	type OkConfig struct {
		Port int `json:",default=8080,range=[1:8080]"`
	}
	var ok OkConfig
	err = UnmarshalJsonMap(map[string]any{}, &ok)
	require.NoError(t, err)
	assert.Equal(t, 8080, ok.Port)
}

// TestFillDefault_RangeOnDefaultValue verifies that the FillDefault path validates
// the default's range as well.
func TestFillDefault_RangeOnDefaultValue(t *testing.T) {
	type BadConfig struct {
		Port int `json:",default=99999,range=[1:8080]"`
	}
	var bad BadConfig
	err := FillDefault(&bad)
	require.Error(t, err)

	type OkConfig struct {
		Port int `json:",default=8080,range=[1:8080]"`
	}
	var ok OkConfig
	require.NoError(t, FillDefault(&ok))
	assert.Equal(t, 8080, ok.Port)
}

// TestUnmarshal_RangeOnEnvDuration verifies that range applies when a duration
// field is overridden through an environment variable.
func TestUnmarshal_RangeOnEnvDuration(t *testing.T) {
	type Config struct {
		Timeout time.Duration `json:",range=[1:10000000000],env=TEST_RANGE_DUR"`
	}

	t.Setenv("TEST_RANGE_DUR", "60s")
	var bad Config
	err := UnmarshalJsonMap(map[string]any{}, &bad)
	require.Error(t, err, "env duration out of range must be rejected")

	t.Setenv("TEST_RANGE_DUR", "5s")
	var ok Config
	err = UnmarshalJsonMap(map[string]any{}, &ok)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, ok.Timeout)
}

// TestValidateRangeForType directly covers the new validation helper.
func TestValidateRangeForType(t *testing.T) {
	rng, err := parseNumberRange("[1:10]")
	require.NoError(t, err)
	opts := &fieldOptions{Range: rng}

	t.Run("int within range", func(t *testing.T) {
		assert.NoError(t, validateRangeForType(reflect.TypeOf(0), 5, opts, "f"))
		assert.NoError(t, validateRangeForType(reflect.TypeOf(0), "5", opts, "f"))
	})
	t.Run("int out of range", func(t *testing.T) {
		assert.Error(t, validateRangeForType(reflect.TypeOf(0), 11, opts, "f"))
		assert.Error(t, validateRangeForType(reflect.TypeOf(0), "11", opts, "f"))
	})
	t.Run("nil range is skipped", func(t *testing.T) {
		assert.NoError(t, validateRangeForType(reflect.TypeOf(0), 999, nil, "f"))
		assert.NoError(t, validateRangeForType(reflect.TypeOf(0), 999, &fieldOptions{}, "f"))
	})
	t.Run("duration compares in nanoseconds", func(t *testing.T) {
		// 5ns lies within [1:10], 1h does not
		assert.NoError(t, validateRangeForType(durationType, 5*time.Nanosecond, opts, "f"))
		assert.NoError(t, validateRangeForType(durationType, "5ns", opts, "f"))
		assert.Error(t, validateRangeForType(durationType, time.Hour, opts, "f"))
		assert.Error(t, validateRangeForType(durationType, "1h", opts, "f"))
	})
	t.Run("duration with unparsable value", func(t *testing.T) {
		assert.Error(t, validateRangeForType(durationType, "not-a-duration", opts, "f"))
	})
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

// --- Additional coverage: bool/uint/float tag defaults via setMatchedPrimitiveValue ---

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
	// Custom types require Convert
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
	// The default value cannot be converted to the target type
	type Config struct {
		Port int `json:",default=abc"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	assert.Error(t, err)
}

func TestUnmarshal_NativeStringToIntField(t *testing.T) {
	// A string value in the map assigned to an int field -> setStringValue converts it
	type Config struct {
		Port int `json:"port"`
	}
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"port": "8080"}, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}
