# conf

配置文件加载与解析包，支持 JSON / YAML 格式，提供默认值填充、环境变量读取、参数验证等能力。

## 特性

- **多格式支持**：`.json`、`.yaml`、`.yml`
- **默认值**：通过 `default` 标签指令为字段设置默认值
- **环境变量**：通过 `env` 标签指令优先从环境变量读取值
- **参数验证**：
  - `range` — 数值范围校验
  - `options` — 枚举值校验
  - `optional` — 标记字段为可选
- **自定义验证**：实现 `Validator` 接口，加载后自动调用
- **环境变量展开**：配置文件中可使用 `${VAR}` 引用环境变量
- **大小写不敏感**：配置文件中的键名与结构体字段名大小写不敏感匹配
- **嵌套结构体**：支持嵌套结构体、匿名嵌入字段、切片、Map 等复杂类型
- **大整数精度**：使用 `json.Number` 保持数值精度，避免大整数丢失精度

## 快速开始

### 安装

```bash
go get github.com/chihqiang/infra-go/conf
```

### 基本用法

定义配置结构体，使用 `json` 标签声明字段名和选项：

```go
package main

import (
    "fmt"
    "time"
    "github.com/chihqiang/infra-go/conf"
)

type Config struct {
    Host     string        `json:",default=0.0.0.0"`
    Port     int           `json:",default=8080"`
    Timeout  time.Duration `json:",default=3s"`
    MaxConns int           `json:",default=10000,range=[1:100000]"`
    LogMode  string        `json:",options=[file,console]"`
    Verbose  bool          `json:",optional"`
}

func main() {
    var cfg Config
    // 出错时 panic
    conf.MustLoad("config.json", &cfg)
    fmt.Printf("%+v\n", cfg)
}
```

`config.json`：

```json
{
    "host": "127.0.0.1",
    "port": 9090,
    "timeout": "5s",
    "maxConns": 50000,
    "logMode": "console"
}
```

等价的 `config.yaml`：

```yaml
host: 127.0.0.1
port: 9090
timeout: 5s
maxConns: 50000
logMode: console
```

## API

### Load

从文件加载配置，返回 error。

```go
func Load(file string, v any, opts ...Option) error
```

```go
var cfg Config
if err := conf.Load("config.yaml", &cfg); err != nil {
    log.Fatal(err)
}
```

### MustLoad

从文件加载配置，出错时 panic。适合在程序启动阶段使用。

```go
func MustLoad(path string, v any, opts ...Option)
```

```go
var cfg Config
conf.MustLoad("config.yaml", &cfg)
```

### LoadFromJSONBytes

从 JSON 字节流加载配置。

```go
func LoadFromJSONBytes(content []byte, v any) error
```

```go
var cfg Config
err := conf.LoadFromJSONBytes([]byte(`{"host": "127.0.0.1", "port": 9090}`), &cfg)
```

### LoadFromYAMLBytes

从 YAML 字节流加载配置。

```go
func LoadFromYAMLBytes(content []byte, v any) error
```

```go
var cfg Config
err := conf.LoadFromYAMLBytes([]byte("host: 127.0.0.1\nport: 9090\n"), &cfg)
```

### FillDefault

仅为结构体填充默认值和环境变量（不从文件加载）。要求结构体所有字段必须为零值。

```go
func FillDefault(v any) error
```

```go
var cfg Config
if err := conf.FillDefault(&cfg); err != nil {
    log.Fatal(err)
}
// cfg.Host == "0.0.0.0", cfg.Port == 8080, cfg.Timeout == 3s ...
```

### UseEnv

`Option` 选项，展开配置文件中的环境变量引用（`${VAR}` 或 `$VAR`）。

```go
func UseEnv() Option
```

```go
// config.json: {"host": "${DB_HOST}", "port": 3306}
os.Setenv("DB_HOST", "db.example.com")

var cfg Config
conf.MustLoad("config.json", &cfg, conf.UseEnv())
// cfg.Host == "db.example.com"
```

## 标签指令

所有指令在 `json` 标签中通过逗号分隔声明，格式为 `json:"key,directive1,directive2,..."`。
当 `key` 为空时，使用字段名作为 key。

### default — 默认值

当配置文件中未提供该字段时，使用默认值填充。

```go
type Config struct {
    Host    string        `json:",default=0.0.0.0"`
    Port    int           `json:",default=8080"`
    Timeout time.Duration `json:",default=3s"`
    Hosts   []string      `json:",default=[a.com,b.com]"`
}
```

支持所有基本类型、`time.Duration`、切片。切片默认值格式为 `[a,b,c]`。

### env — 环境变量

优先从指定环境变量读取值，如果环境变量为空则回退到配置文件。

```go
type Config struct {
    Name string `json:",env=APP_NAME"`
    Port int    `json:",default=8080"`
}
```

```bash
APP_NAME=myapp ./myapp
```

### optional — 可选字段

标记字段为可选，配置文件中未提供时不报错。

```go
type Config struct {
    Name    string `json:"name"`        // 必填
    Verbose bool   `json:",optional"`   // 可选
}
```

### range — 数值范围

校验数值是否在指定范围内，格式为 `[left:right]`，支持开闭区间。

```go
type Config struct {
    Port     int   `json:",range=[1:65535]"`      // 1 ≤ port ≤ 65535
    MaxConns int   `json:",range=[1:100000]"`     // 1 ≤ maxConns ≤ 100000
    Level    int64 `json:",range=[0:1000)"`       // 0 ≤ level < 1000
    Ratio    float64 `json:",range=(0:1]"`        // 0 < ratio ≤ 1
}
```

区间符号说明：

| 符号 | 含义 |
| ------ | ------ |
| `[` | 闭区间，包含左边界 |
| `(` | 开区间，不包含左边界 |
| `]` | 闭区间，包含右边界 |
| `)` | 开区间，不包含右边界 |

省略边界时表示无限制：`[:100]` 表示 ≤ 100，`[1:]` 表示 ≥ 1。

### options — 枚举值

校验值是否在允许的选项列表中。

```go
type Config struct {
    LogMode string `json:",options=[file,console]"`
    Env     string `json:",options=[dev|staging|prod]"`  // 也可用 | 分隔
}
```

### string — 从字符串解析

强制将配置值转为字符串后再解析，适用于值类型不匹配时的自动转换。

```go
type Config struct {
    Port int `json:"port,string"`
}
```

```json
{"port": "9090"}  // 字符串 "9090" 会被解析为 int 9090
```

## 自定义验证

实现 `Validator` 接口，在配置加载完成后自动执行自定义验证逻辑。

```go
type Validator interface {
    Validate() error
}
```

```go
type ServerConfig struct {
    Port int `json:"port"`
}

func (c ServerConfig) Validate() error {
    if c.Port <= 1024 {
        return fmt.Errorf("port must be > 1024, got %d", c.Port)
    }
    return nil
}

// 加载配置时会自动调用 Validate()
var cfg ServerConfig
err := conf.Load("config.json", &cfg)
// 如果 Port <= 1024，err 包含 "port must be > 1024"
```

## 嵌套结构体

支持任意层级的嵌套结构体，每个层级的字段都会独立应用默认值和验证。

```go
type Database struct {
    Host string `json:",default=localhost"`
    Port int    `json:",default=3306"`
}

type Redis struct {
    Host string `json:",default=127.0.0.1"`
    Port int    `json:",default=6379"`
}

type AppConfig struct {
    Name   string   `json:"name"`
    DB     Database `json:"db"`
    Redis  Redis    `json:"redis"`
}
```

```json
{
    "name": "myapp",
    "db": {
        "port": 5432
    }
}
```

加载后 `DB.Host` 使用默认值 `localhost`，`Redis` 全部使用默认值。

## 匿名嵌入字段

支持匿名嵌入结构体，嵌入的字段会被展平处理。

```go
type Base struct {
    Host string `json:",default=0.0.0.0"`
    Port int    `json:",default=8080"`
}

type Server struct {
    Base
    Name string `json:"name"`
}
```

```json
{"name": "api-server"}
```

加载后 `Server.Host` 为 `0.0.0.0`，`Server.Port` 为 `8080`。

## 切片与 Map

```go
type Config struct {
    Hosts  []string          `json:"hosts"`
    Ports  []int             `json:"ports"`
    Labels map[string]string `json:"labels"`
}
```

```json
{
    "hosts": ["a.com", "b.com"],
    "ports": [8080, 9090],
    "labels": {"env": "prod", "zone": "us-east-1"}
}
```

## 大整数精度

内部使用 `json.Number` 保持数值精度，不会丢失大整数精度。

```go
type Config struct {
    ID        int64 `json:"id"`
    Timestamp int64 `json:"timestamp"`
}
```

```json
{
    "id": 1234567890123456789,
    "timestamp": 9223372036854775807
}
```

## 完整示例

```go
package main

import (
    "fmt"
    "time"
    "os"

    "github.com/chihqiang/infra-go/conf"
)

type Database struct {
    Host     string `json:",default=localhost"`
    Port     int    `json:",default=3306"`
    User     string `json:"user"`
    Password string `json:"password"`
}

type ServerConfig struct {
    Host         string        `json:",default=0.0.0.0"`
    Port         int           `json:",default=8080,range=[1:65535]"`
    Timeout      time.Duration `json:",default=3s"`
    MaxConns     int           `json:",default=10000,range=[1:100000]"`
    LogMode      string        `json:",options=[file,console]"`
    Verbose      bool          `json:",optional"`
    DB           Database      `json:"db"`
}

func (c ServerConfig) Validate() error {
    if c.Port == c.DB.Port {
        return fmt.Errorf("server port and db port must not be the same")
    }
    return nil
}

func main() {
    // 从环境变量引用
    os.Setenv("DB_PASSWORD", "secret")

    var cfg ServerConfig
    conf.MustLoad("config.yaml", &cfg, conf.UseEnv())

    fmt.Printf("Server: %s:%d\n", cfg.Host, cfg.Port)
    fmt.Printf("DB: %s:%d\n", cfg.DB.Host, cfg.DB.Port)
}
```

`config.yaml`：

```yaml
host: 127.0.0.1
port: 9090
timeout: 5s
logMode: console
db:
  user: root
  password: ${DB_PASSWORD}
  port: 3306
```
