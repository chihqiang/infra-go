package mapping

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeref(t *testing.T) {
	assert.Equal(t, "int", Deref(reflect.TypeOf(int(1))).Kind().String())
	assert.Equal(t, "string", Deref(reflect.TypeOf((*string)(nil))).Kind().String())
}

func TestValidatePtr(t *testing.T) {
	var i int
	assert.NoError(t, ValidatePtr(reflect.ValueOf(&i)))
	assert.Error(t, ValidatePtr(reflect.ValueOf(i)))
	assert.Error(t, ValidatePtr(reflect.ValueOf((*int)(nil))))
}

func TestParseKeyAndOptions_NoTag(t *testing.T) {
	type S struct {
		Name string
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "Name", key)
	assert.Nil(t, opts)
}

func TestParseKeyAndOptions_SimpleKey(t *testing.T) {
	type S struct {
		Name string `json:"name"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "name", key)
	assert.Nil(t, opts)
}

func TestParseKeyAndOptions_Default(t *testing.T) {
	type S struct {
		Name string `json:",default=hello"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "Name", key)
	assert.NotNil(t, opts)
	assert.Equal(t, "hello", opts.Default)
}

func TestParseKeyAndOptions_Optional(t *testing.T) {
	type S struct {
		Port int `json:",optional"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.True(t, opts.Optional)
}

func TestParseKeyAndOptions_Env(t *testing.T) {
	type S struct {
		Name string `json:",env=APP_NAME"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "APP_NAME", opts.EnvVar)
}

func TestParseKeyAndOptions_Options(t *testing.T) {
	type S struct {
		Mode string `json:",options=[file,console]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, []string{"file", "console"}, opts.Options)
}

func TestParseKeyAndOptions_Range(t *testing.T) {
	type S struct {
		Port int `json:",range=[0:65535]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.NotNil(t, opts.Range)
	assert.True(t, opts.Range.leftInclude)
	assert.True(t, opts.Range.rightInclude)
	assert.Equal(t, 0.0, opts.Range.left)
	assert.Equal(t, 65535.0, opts.Range.right)
}

func TestParseKeyAndOptions_RangeOpen(t *testing.T) {
	type S struct {
		Port int `json:",range=[0:1000)"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.True(t, opts.Range.leftInclude)
	assert.False(t, opts.Range.rightInclude)
}

func TestParseKeyAndOptions_Combined(t *testing.T) {
	type S struct {
		Port int `json:"port,default=8080,range=[1:65535]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "port", key)
	assert.Equal(t, "8080", opts.Default)
	assert.NotNil(t, opts.Range)
}

func TestParseNumberRange(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"[0:100]", false},
		{"(0:100]", false},
		{"[0:100)", false},
		{"(0:100)", false},
		{"[:100]", false},
		{"[0:]", false},
		{"[100:0]", true},
		{"[2:2)", true},
		{"[2:2]", false},
		{"", true},
		{"abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseNumberRange(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsInRange(t *testing.T) {
	opts := &fieldOptions{Range: &numberRange{left: 0, leftInclude: true, right: 100, rightInclude: false}}
	assert.True(t, opts.isInRange(0))
	assert.True(t, opts.isInRange(50))
	assert.True(t, opts.isInRange(99))
	assert.False(t, opts.isInRange(100))
	assert.False(t, opts.isInRange(-1))

	opts2 := &fieldOptions{}
	assert.True(t, opts2.isInRange(999999))

	opts3 := &fieldOptions{Range: &numberRange{left: 0, leftInclude: false, right: 100, rightInclude: true}}
	assert.False(t, opts3.isInRange(0))
	assert.True(t, opts3.isInRange(100))
}

func TestConvertTypeFromString(t *testing.T) {
	v, err := convertTypeFromString(reflect.Int, "42")
	assert.NoError(t, err)
	assert.Equal(t, int64(42), v)

	v, err = convertTypeFromString(reflect.Bool, "true")
	assert.NoError(t, err)
	assert.Equal(t, true, v)

	v, err = convertTypeFromString(reflect.Bool, "1")
	assert.NoError(t, err)
	assert.Equal(t, true, v)

	v, err = convertTypeFromString(reflect.Float64, "3.14")
	assert.NoError(t, err)
	assert.Equal(t, float64(3.14), v)

	_, err = convertTypeFromString(reflect.Int, "abc")
	assert.Error(t, err)
}

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

func TestStructValueRequired(t *testing.T) {
	type RequiredStruct struct {
		Name string `json:"name"`
	}
	assert.True(t, structValueRequired("json", reflect.TypeOf(RequiredStruct{})))

	type OptionalStruct struct {
		Name string `json:",optional"`
	}
	assert.False(t, structValueRequired("json", reflect.TypeOf(OptionalStruct{})))

	type DefaultStruct struct {
		Name string `json:",default=hello"`
	}
	assert.False(t, structValueRequired("json", reflect.TypeOf(DefaultStruct{})))
}


