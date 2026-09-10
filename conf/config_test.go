package conf

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestExpandEnv(t *testing.T) {
	t.Setenv("EXP_ENV_SET", "world")
	t.Setenv("EXP_ENV_EMPTY", "")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text unchanged", "hello", "hello"},
		{"dollar syntax", "$EXP_ENV_SET", "world"},
		{"braced syntax", "${EXP_ENV_SET}", "world"},
		{"var not set -> empty", "x=${EXP_NOT_SET}y", "x=y"},
		{"var empty -> empty", "[${EXP_ENV_EMPTY}]", "[]"},
		// ${VAR:-default}
		{"default var not set", "${EXP_NOT_SET:-fallback}", "fallback"},
		{"default var empty", "${EXP_ENV_EMPTY:-fallback}", "fallback"}, // shell :- 语义：空也回退
		{"default var set", "${EXP_ENV_SET:-fallback}", "world"},
		{"default mixed with text", "hi ${EXP_NOT_SET:-you}! ${EXP_ENV_SET}", "hi you! world"},
		{"no default -> empty", "${EXP_NOT_SET}", ""},
		{"nested literal not re-expanded", "${EXP_NOT_SET:-${EXP_ENV_SET}}", "${EXP_ENV_SET}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExpandEnv(tt.in))
		})
	}
}

// TestExpandEnv_DollarEscape 验证 $$ 转义为字面量 $，
// 使配置中能够书写字面 $（否则 `${...}` 之外的 $ 会被当作变量引用吞掉）。
func TestExpandEnv_DollarEscape(t *testing.T) {
	t.Setenv("EE_VAR", "val")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare escape", "$$", "$"},
		{"escape inside word", "p$$ssword", "p$ssword"},
		{"escape does not consume following name", "$$EE_VAR", "$EE_VAR"},
		{"escape mixed with real var", "100$$-${EE_VAR}", "100$-val"},
		{"double escape", "$$$$", "$$"},
		{"trailing dollar stays literal", "100$", "100$"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExpandEnv(tt.in))
		})
	}
}

// TestLoad_UseEnvDoesNotSwallowLiteralDollar 回归测试：开启 UseEnv 后，
// 配置中的字面 $ 不再被当作变量引用吞掉。
// 旧实现在解析前对原文做文本替换，`"p$ssword"` 会被展开成 `"p"`。
func TestLoad_UseEnvDoesNotSwallowLiteralDollar(t *testing.T) {
	file := createTempFile(t, ".json", `{"password": "p$$ssword"}`)

	var cfg struct {
		Password string `json:"password"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, "p$ssword", cfg.Password)
}

// TestLoad_UseEnvCannotInjectStructure 回归测试：环境变量的值不得注入/改写配置结构。
// 旧实现在解析前做文本替换，值中的 JSON 片段会凭空创建出新的键。
func TestLoad_UseEnvCannotInjectStructure(t *testing.T) {
	t.Setenv("INJECT_ME", `","admin":true,"x":"`)

	file := createTempFile(t, ".json", `{"user": "${INJECT_ME}"}`)

	var cfg struct {
		User string `json:"user"`
		// 该键只能来自配置文件本身；若被环境变量凭空创建，下面断言会失败
		Admin bool `json:"admin,optional"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, `","admin":true,"x":"`, cfg.User)
	assert.False(t, cfg.Admin, "env value must not inject new config keys")
}

// TestLoad_UseEnvSpecialCharsInValue 回归测试：环境变量值中的引号、大括号、
// 逗号等字符不再破坏配置文件语法（旧实现下会导致解析失败）。
func TestLoad_UseEnvSpecialCharsInValue(t *testing.T) {
	t.Setenv("QUOTED_VAL", `he said "hi" and left`)
	t.Setenv("BRACE_VAL", `{"a":1}`)
	t.Setenv("COMMA_VAL", "a,b,c")
	t.Setenv("NEWLINE_VAL", "line1\nline2")

	text := `{"msg": "${QUOTED_VAL}", "raw": "${BRACE_VAL}", "list": "${COMMA_VAL}", "multi": "${NEWLINE_VAL}"}`
	file := createTempFile(t, ".json", text)

	var cfg struct {
		Msg   string `json:"msg"`
		Raw   string `json:"raw"`
		List  string `json:"list"`
		Multi string `json:"multi"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, `he said "hi" and left`, cfg.Msg)
	assert.Equal(t, `{"a":1}`, cfg.Raw)
	assert.Equal(t, "a,b,c", cfg.List)
	assert.Equal(t, "line1\nline2", cfg.Multi)
}

// TestLoad_UseEnvSpecialCharsInYAML 同上，覆盖 YAML（值中的冒号/引号不再破坏语法）。
func TestLoad_UseEnvSpecialCharsInYAML(t *testing.T) {
	t.Setenv("URL_VAL", "https://user:pass@host:5432/db?sslmode=disable")
	t.Setenv("JSON_VAL", `{"k": "v"}`)

	text := "dsn: ${URL_VAL}\nmeta: ${JSON_VAL}\n"
	file := createTempFile(t, ".yaml", text)

	var cfg struct {
		DSN  string `json:"dsn"`
		Meta string `json:"meta"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, "https://user:pass@host:5432/db?sslmode=disable", cfg.DSN)
	assert.Equal(t, `{"k": "v"}`, cfg.Meta)
}

// TestLoad_UseEnvExpandsMapKeys 验证 map 的键同样支持环境变量展开（与旧行为一致）。
func TestLoad_UseEnvExpandsMapKeys(t *testing.T) {
	t.Setenv("MAP_KEY", "dynamic")
	t.Setenv("MAP_VAL", "v")

	file := createTempFile(t, ".json", `{"labels": {"${MAP_KEY}": "${MAP_VAL}"}}`)

	var cfg struct {
		Labels map[string]string `json:"labels"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, map[string]string{"dynamic": "v"}, cfg.Labels)
}

// TestLoad_UseEnvDuplicateKeyAfterExpansion 验证展开后键冲突会报错，
// 而不是静默丢弃其中一个键的数据。
func TestLoad_UseEnvDuplicateKeyAfterExpansion(t *testing.T) {
	t.Setenv("DUP_KEY", "same")

	file := createTempFile(t, ".json", `{"m": {"${DUP_KEY}": "a", "same": "b"}}`)

	var cfg struct {
		M map[string]string `json:"m"`
	}
	err := Load(file, &cfg, UseEnv())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate key")
}

// TestLoad_UseEnvNestedAndSlices 验证嵌套结构体与切片元素中的引用也会被展开。
func TestLoad_UseEnvNestedAndSlices(t *testing.T) {
	t.Setenv("NEST_HOST", "h")
	t.Setenv("LIST_ITEM", "item")

	text := `{"db": {"host": "${NEST_HOST}"}, "items": ["${LIST_ITEM}", "plain"]}`
	file := createTempFile(t, ".json", text)

	var cfg struct {
		DB struct {
			Host string `json:"host"`
		} `json:"db"`
		Items []string `json:"items"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, "h", cfg.DB.Host)
	assert.Equal(t, []string{"item", "plain"}, cfg.Items)
}

// TestLoad_UseEnvDoesNotReexpandResult 验证展开结果不会被二次展开
// （环境变量的值里含 ${...} 时应原样保留）。
func TestLoad_UseEnvDoesNotReexpandResult(t *testing.T) {
	t.Setenv("INNER_REF", "${OTHER_VAR}")
	t.Setenv("OTHER_VAR", "should-not-appear")

	file := createTempFile(t, ".json", `{"host": "${INNER_REF}"}`)

	var cfg struct {
		Host string `json:"host"`
	}
	require.NoError(t, Load(file, &cfg, UseEnv()))
	assert.Equal(t, "${OTHER_VAR}", cfg.Host)
}

// TestLoadFromBytes_UseEnv 验证字节入口同样支持 opts（UseEnv）。
func TestLoadFromBytes_UseEnv(t *testing.T) {
	t.Setenv("BYTES_HOST", "from-bytes")

	var cfg struct {
		Host string `json:"host"`
	}
	require.NoError(t, LoadFromJSONBytes([]byte(`{"host": "${BYTES_HOST}"}`), &cfg, UseEnv()))
	assert.Equal(t, "from-bytes", cfg.Host)

	var yamlCfg struct {
		Host string `json:"host"`
	}
	require.NoError(t, LoadFromYAMLBytes([]byte("host: ${BYTES_HOST}\n"), &yamlCfg, UseEnv()))
	assert.Equal(t, "from-bytes", yamlCfg.Host)
}

// TestLoadFromBytes_NoOptsKeepsLiteral 验证不传 opts 时配置中的 $ 原样保留。
func TestLoadFromBytes_NoOptsKeepsLiteral(t *testing.T) {
	t.Setenv("BYTES_HOST", "from-bytes")

	var raw struct {
		Host string `json:"host"`
	}
	require.NoError(t, LoadFromJSONBytes([]byte(`{"host": "${BYTES_HOST}"}`), &raw))
	assert.Equal(t, "${BYTES_HOST}", raw.Host)
}

// TestLoad_UseEnvDisabledKeepsLiteral 验证未开启 UseEnv 时配置中的 $ 原样保留。
func TestLoad_UseEnvDisabledKeepsLiteral(t *testing.T) {
	file := createTempFile(t, ".json", `{"password": "p$ssword", "host": "${DB_HOST}"}`)

	var cfg struct {
		Password string `json:"password"`
		Host     string `json:"host"`
	}
	require.NoError(t, Load(file, &cfg))
	assert.Equal(t, "p$ssword", cfg.Password)
	assert.Equal(t, "${DB_HOST}", cfg.Host)
}

func TestLoad_UseEnvExpansionDefaultValue(t *testing.T) {
	// 未设置环境变量时回退到 :- 默认值
	text := `{
		"host": "${DB_HOST:-fallback.example.com}",
		"port": "${DB_PORT:-3306}"
	}`

	type EnvExpConfig struct {
		Host string `json:"host"`
		Port string `json:"port"`
	}

	file := createTempFile(t, ".json", text)
	var cfg EnvExpConfig
	err := Load(file, &cfg, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "fallback.example.com", cfg.Host) // 未设置 → 默认值
	assert.Equal(t, "3306", cfg.Port)

	// 设置了环境变量 → 优先使用环境变量
	t.Setenv("DB_HOST", "db.example.com")
	var cfg2 EnvExpConfig
	err = Load(file, &cfg2, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "db.example.com", cfg2.Host)
	assert.Equal(t, "3306", cfg2.Port)
}

func TestLoad_UseEnvExpansionYAMLDefault(t *testing.T) {
	// 用户场景：YAML 嵌套结构 + 密钥/签发者默认值
	text := "jwt:\n  secret: ${JWT_SECRET:-dev-secret}\n  issuer: ${JWT_ISSUER:-my-app}\n"

	type JWT struct {
		Secret string `json:"secret"`
		Issuer string `json:"issuer"`
	}
	type AppConfig struct {
		JWT JWT `json:"jwt"`
	}

	file := createTempFile(t, ".yaml", text)
	var cfg AppConfig
	err := Load(file, &cfg, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "dev-secret", cfg.JWT.Secret) // 未设置 → 默认值
	assert.Equal(t, "my-app", cfg.JWT.Issuer)

	// 环境变量为空字符串时也应回退默认值（shell :- 语义）
	t.Setenv("JWT_SECRET", "")
	var cfg2 AppConfig
	err = Load(file, &cfg2, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "dev-secret", cfg2.JWT.Secret)

	// 设置了非空环境变量 → 覆盖默认值
	t.Setenv("JWT_SECRET", "real-secret")
	t.Setenv("JWT_ISSUER", "prod")
	var cfg3 AppConfig
	err = Load(file, &cfg3, UseEnv())
	assert.NoError(t, err)
	assert.Equal(t, "real-secret", cfg3.JWT.Secret)
	assert.Equal(t, "prod", cfg3.JWT.Issuer)
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
		Port int     `json:"port"`
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
