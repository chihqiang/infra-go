package conf

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// createTempFile 创建临时配置文件用于测试。
func createTempFile(t *testing.T, ext, text string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp(os.TempDir(), "config*"+ext)
	assert.NoError(t, err)
	_, err = tmpFile.Write([]byte(text))
	assert.NoError(t, err)
	filename := tmpFile.Name()
	assert.NoError(t, tmpFile.Close())
	t.Cleanup(func() { _ = os.Remove(filename) })
	return filename
}

// TestConfig 定义测试配置结构体。
type TestConfig struct {
	Host         string        `json:",default=0.0.0.0"`
	Port         int           `json:",default=8080"`
	Timeout      time.Duration `json:",default=3s"`
	MaxConns     int           `json:",default=10000,range=[1:100000]"`
	LogMode      string        `json:",options=[file,console]"`
	Verbose      bool          `json:",optional"`
	CpuThreshold int64         `json:",default=900,range=[0:1000)"`
}

func TestLoad_JSON(t *testing.T) {
	text := `{
		"host": "127.0.0.1",
		"port": 9090,
		"timeout": "5s",
		"maxConns": 50000,
		"logMode": "console",
		"cpuThreshold": 500
	}`

	file := createTempFile(t, ".json", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, 50000, cfg.MaxConns)
	assert.Equal(t, "console", cfg.LogMode)
	assert.Equal(t, int64(500), cfg.CpuThreshold)
}

func TestLoad_YAML(t *testing.T) {
	text := `
host: 127.0.0.1
port: 9090
timeout: 5s
maxConns: 50000
logMode: console
cpuThreshold: 500
`
	file := createTempFile(t, ".yaml", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, 50000, cfg.MaxConns)
	assert.Equal(t, "console", cfg.LogMode)
	assert.Equal(t, int64(500), cfg.CpuThreshold)
}

func TestLoad_YML(t *testing.T) {
	text := `
host: 127.0.0.1
port: 9090
timeout: 5s
maxConns: 50000
logMode: console
cpuThreshold: 500
`
	file := createTempFile(t, ".yml", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, "console", cfg.LogMode)
}

func TestLoad_DefaultValues(t *testing.T) {
	text := `{
		"logMode": "file"
	}`

	file := createTempFile(t, ".json", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Host) // default
	assert.Equal(t, 8080, cfg.Port)      // default
	assert.Equal(t, 3*time.Second, cfg.Timeout)
	assert.Equal(t, 10000, cfg.MaxConns)
	assert.Equal(t, "file", cfg.LogMode)
	assert.Equal(t, int64(900), cfg.CpuThreshold)
}

func TestLoad_RangeError(t *testing.T) {
	text := `{
		"logMode": "file",
		"maxConns": 0
	}`

	file := createTempFile(t, ".json", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestLoad_OptionsError(t *testing.T) {
	text := `{
		"logMode": "invalid"
	}`

	file := createTempFile(t, ".json", text)
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed options")
}

func TestLoad_RequiredFieldMissing(t *testing.T) {
	type RequiredConfig struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}

	file := createTempFile(t, ".json", `{"port": 8080}`)
	var cfg RequiredConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not set")
}

func TestLoad_OptionalField(t *testing.T) {
	type OptionalConfig struct {
		Name string `json:"name"`
		Port int    `json:",optional"`
	}

	file := createTempFile(t, ".json", `{"name": "test"}`)
	var cfg OptionalConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 0, cfg.Port)
}

func TestLoad_EnvVar(t *testing.T) {
	type EnvConfig struct {
		Name string `json:",env=APP_NAME"`
		Port int    `json:"port,default=8080"`
	}

	t.Setenv("APP_NAME", "myapp")

	file := createTempFile(t, ".json", `{"port": 9090}`)
	var cfg EnvConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "myapp", cfg.Name) // 从环境变量读取
	assert.Equal(t, 9090, cfg.Port)
}

func TestLoad_UseEnvExpansion(t *testing.T) {
	t.Setenv("DB_HOST", "db.example.com")

	text := `{
		"host": "${DB_HOST}",
		"port": 3306
	}`

	type EnvExpConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	file := createTempFile(t, ".json", text)
	var cfg EnvExpConfig
	err := Load(file, &cfg, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "db.example.com", cfg.Host)
	assert.Equal(t, 3306, cfg.Port)
}

// validatorTestConfig 实现 Validator 接口
type validatorTestConfig struct {
	Port int `json:"port"`
}

func (c validatorTestConfig) Validate() error {
	if c.Port <= 1024 {
		return validatorTestError{"port must be > 1024"}
	}
	return nil
}

type validatorTestError struct{ msg string }

func (e validatorTestError) Error() string { return e.msg }

func TestLoad_Validator(t *testing.T) {
	file := createTempFile(t, ".json", `{"port": 80}`)
	var cfg validatorTestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port must be > 1024")
}

func TestLoad_ValidatorPass(t *testing.T) {
	file := createTempFile(t, ".json", `{"port": 8080}`)
	var cfg validatorTestConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestFillDefault(t *testing.T) {
	type DefaultConfig struct {
		Host string        `json:",default=localhost"`
		Port int           `json:",default=3306"`
		TTL  time.Duration `json:",default=10s"`
	}

	var cfg DefaultConfig
	err := FillDefault(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 3306, cfg.Port)
	assert.Equal(t, 10*time.Second, cfg.TTL)
}

func TestFillDefault_EnvVar(t *testing.T) {
	type EnvDefaultConfig struct {
		Host string `json:",default=localhost,env=DB_HOST"`
		Port int    `json:",default=3306"`
	}

	t.Setenv("DB_HOST", "envhost")
	var cfg EnvDefaultConfig
	err := FillDefault(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, "envhost", cfg.Host)
	assert.Equal(t, 3306, cfg.Port)
}

func TestFillDefault_NotZero(t *testing.T) {
	type DefaultConfig struct {
		Host string `json:",default=localhost"`
	}

	cfg := DefaultConfig{Host: "already-set"}
	err := FillDefault(&cfg)
	assert.Error(t, err)
}

func TestLoad_NestedStruct(t *testing.T) {
	type Database struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=3306"`
	}

	type AppConfig struct {
		Name string   `json:"name"`
		DB   Database `json:"db"`
	}

	text := `{
		"name": "myapp",
		"db": {
			"port": 5432
		}
	}`

	file := createTempFile(t, ".json", text)
	var cfg AppConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "myapp", cfg.Name)
	assert.Equal(t, "localhost", cfg.DB.Host) // default
	assert.Equal(t, 5432, cfg.DB.Port)
}

func TestLoad_AnonymousField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
		Port int    `json:",default=8080"`
	}

	type Server struct {
		Base
		Name string `json:"name"`
	}

	text := `{"name": "api-server"}`
	file := createTempFile(t, ".json", text)
	var cfg Server
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "api-server", cfg.Name)
}

func TestLoad_SliceField(t *testing.T) {
	type SliceConfig struct {
		Hosts []string `json:"hosts"`
		Ports []int    `json:"ports"`
	}

	text := `{
		"hosts": ["a.com", "b.com"],
		"ports": [8080, 9090]
	}`

	file := createTempFile(t, ".json", text)
	var cfg SliceConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a.com", "b.com"}, cfg.Hosts)
	assert.Equal(t, []int{8080, 9090}, cfg.Ports)
}

func TestLoad_MapField(t *testing.T) {
	type MapConfig struct {
		Labels map[string]string `json:"labels"`
	}

	text := `{
		"labels": {"env": "prod", "zone": "us-east-1"}
	}`

	file := createTempFile(t, ".json", text)
	var cfg MapConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "prod", cfg.Labels["env"])
	assert.Equal(t, "us-east-1", cfg.Labels["zone"])
}

func TestLoad_UnsupportedExtension(t *testing.T) {
	file := createTempFile(t, ".ini", "key=value")
	var cfg TestConfig
	err := Load(file, &cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config file type")
}

func TestLoad_FileNotFound(t *testing.T) {
	var cfg TestConfig
	err := Load("/nonexistent/path.json", &cfg)
	assert.Error(t, err)
}

func TestMustLoad_Panics(t *testing.T) {
	assert.Panics(t, func() {
		var cfg TestConfig
		MustLoad("/nonexistent/path.json", &cfg)
	})
}

func TestLoadFromJSONBytes(t *testing.T) {
	content := []byte(`{"name": "test", "port": 8080}`)
	var cfg struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	err := LoadFromJSONBytes(content, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
}

func TestLoadFromYAMLBytes(t *testing.T) {
	content := []byte("name: test\nport: 8080\n")
	var cfg struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	err := LoadFromYAMLBytes(content, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
}

func TestLoad_PointerField(t *testing.T) {
	type PtrConfig struct {
		Host *string `json:"host,default=localhost"`
		Port int      `json:"port"`
	}

	text := `{"port": 8080}`
	file := createTempFile(t, ".json", text)
	var cfg PtrConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestLoad_DefaultSlice(t *testing.T) {
	type SliceDefaultConfig struct {
		Hosts []string `json:",default=[a.com,b.com]"`
	}

	var cfg SliceDefaultConfig
	err := FillDefault(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a.com", "b.com"}, cfg.Hosts)
}

func TestLoad_LargeIntegers(t *testing.T) {
	type LargeIntConfig struct {
		ID        int64 `json:"id"`
		Timestamp int64 `json:"timestamp"`
	}

	text := `{
		"id": 1234567890123456789,
		"timestamp": 9223372036854775807
	}`

	file := createTempFile(t, ".json", text)
	var cfg LargeIntConfig
	err := Load(file, &cfg)
	assert.NoError(t, err)
	assert.Equal(t, int64(1234567890123456789), cfg.ID)
	assert.Equal(t, int64(9223372036854775807), cfg.Timestamp)
}
