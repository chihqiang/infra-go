package mapping

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
