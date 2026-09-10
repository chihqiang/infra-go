# mapping

结构体标签解析与 `map[string]any` 反序列化工具包。

支持通过结构体标签定义默认值、环境变量、可选字段、枚举验证、范围验证等扩展功能，是 `conf` 及其他包填充默认配置的底层基础。

## 特性

- **默认值**：`default=xxx` 标签，未提供值时自动填充
- **环境变量**：`env=VAR_NAME` 标签，优先从环境变量读取
- **可选字段**：`optional` 标签，未提供值时跳过而不报错
- **枚举验证**：`options=[a,b,c]` 标签，限制字段值范围
- **范围验证**：`range=[0:65535]` 标签，数值范围校验
- **字符串模式**：`string` 标签，强制从字符串解析值
- **大小写不敏感**：`WithCanonicalKeyFunc` 选项，支持配置 key 大小写不敏感匹配
- **仅填充默认值**：`WithDefault()` 选项，用于零值结构体填充默认配置
- **填充并覆盖**：`FillAndOverride` / `MustFillAndOverride`，先填充默认值再用用户配置的非零字段覆盖，消除各模块重复的 `fillDefault` 代码
- **类型丰富**：支持基本类型、`time.Duration`、切片、map、指针、嵌套结构体、匿名嵌入结构体

## 安装

```bash
go get github.com/chihqiang/infra-go/mapping
```

## 快速开始

```go
package main

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/mapping"
)

type Config struct {
    Host    string        `json:"host,default=localhost"`
    Port    int           `json:"port,default=8080,range=[1:65535]"`
    Timeout time.Duration `json:"timeout,default=5s"`
    Mode    string        `json:"mode,default=dev,options=[dev,prod,test]"`
}

func main() {
    m := map[string]any{
        "port": json.Number("9090"),
    }

    var cfg Config
    if err := mapping.UnmarshalJsonMap(m, &cfg); err != nil {
        panic(err)
    }

    fmt.Printf("Host: %s\n", cfg.Host)         // localhost
    fmt.Printf("Port: %d\n", cfg.Port)          // 9090
    fmt.Printf("Timeout: %v\n", cfg.Timeout)    // 5s
    fmt.Printf("Mode: %s\n", cfg.Mode)          // dev
}
```

## 结构体标签

所有标签选项使用逗号分隔，写在 `json` 标签值中（第一个段为字段名，后续段为选项）。

### 标签格式

```text
`json:"<字段名>,<选项1>,<选项2>,..."`
```

如果不需要指定字段名（使用结构体字段名），可以用逗号开头：

```text
`json:",default=localhost"`
```

### 支持的标签选项

| 选项 | 格式 | 说明 |
| ------ | ------ | ------ |
| `default` | `default=<值>` | 默认值，未提供时自动填充 |
| `env` | `env=<变量名>` | 环境变量名，优先从环境变量读取 |
| `optional` | `optional` / `optional=dep` / `optional=!dep` | 字段可选；带依赖时条件可选 |
| `options` | `options=[a,b,c]` 或 `options=a\|b\|c` | 枚举验证，值必须在列表中 |
| `range` | `range=[min:max]` | 数值范围验证 |
| `string` | `string` | 强制从字符串模式解析值 |

> **未知选项会报错**。标签中的拼写错误（如 `optinal`）或前缀误写
> （如 `defaultFoo=bar`）会直接返回 `unknown option` 错误，而不是被静默忽略。
> 这使得“标签写错导致字段行为不符预期”能在加载期被发现，
> 而不是等到运行期以 `field not set` 的形式暴露。
>
> 历史上 `inherit` 会被解析但从未生效，现已明确报错（本设计中没有“父级”概念）：
> 如果确实需要继承语义，请显式写出值或给字段加 `default`。

### 默认值（default）

```go
type Config struct {
    Host string `json:"host,default=localhost"`
    Port int    `json:"port,default=8080"`
}
```

支持切片默认值（JSON 数组或方括号分隔）：

```go
type Config struct {
    Hosts []string `json:"hosts,default=[a.com,b.com]"`
}
```

### 环境变量（env）

环境变量优先级高于默认值和配置文件中的值：

```go
type Config struct {
    Name string `json:"name,env=APP_NAME"`
}
// 如果环境变量 APP_NAME 已设置，使用其值
```

### 可选字段（optional）

未标记 `optional` 且未提供值的字段会报错。标记后未提供值时跳过：

```go
type Config struct {
    Name string `json:"name"`           // 必填
    Port int    `json:",optional"`      // 可选，未提供时为零值
}
```

#### 条件可选（optional=dep）

可以声明“仅在某个依赖被设置时才可选”，依赖名为**配置键**
（即依赖字段的 json/yaml 标签键）：

```go
type Config struct {
    // 依赖存在时可选：给了 use_proxy 就可以不写 proxy_url
    UseProxy bool   `json:"use_proxy,optional"`
    ProxyURL string `json:"proxy_url,optional=use_proxy"`

    // 依赖不存在时可选：没给 mode 就不需要提供 custom_mode
    Mode       string `json:"mode,optional"`
    CustomMode string `json:"custom_mode,optional=!mode"`
}
```

语义与覆盖规则：

| 标签 | 可选条件 | 条件不满足时 |
|------|---------|-------------|
| `optional=dep` | `dep` **已设置** | 未提供值则报错（视为必填） |
| `optional=!dep` | `dep` **未设置** | 未提供值则报错（视为必填） |

- 依赖名必须能匹配同一结构体（含匿名嵌入字段）中的某个字段键，
  否则返回 `does not match any field key` 错误 —— 避免依赖名写错导致
  条件可选静默失效。
- 依赖查找与字段取值走同一路径：配置 `WithCanonicalKeyFunc(strings.ToLower)`
  时同样大小写不敏感。
- 字段同时有 `default` 时**忽略依赖检查**：默认值总能填上值，依赖与之无关。
- 字段自己提供了值时不做依赖判定。

### 枚举验证（options）

```go
type Config struct {
    Mode string `json:"mode,options=[dev,prod,test]"`
    // 或使用管道分隔：json:"mode,options=dev|prod|test"
}
// 值不在列表中时会报错
```

### 范围验证（range）

支持闭区间 `[min:max]` 和开区间 `(min:max)`，以及混合：

```go
type Config struct {
    Port    int     `json:"port,range=[1:65535]"`       // 1 ≤ port ≤ 65535
    Score   float64 `json:"score,range=[0:100)"`        // 0 ≤ score < 100
    Age     int     `json:"age,range=(0:150]"`          // 0 < age ≤ 150
    Temperature float64 `json:"temp,range=[:100]"`      // temp ≤ 100
    Discount float64 `json:"discount,range=[0:]"`       // discount ≥ 0
}
```

格式说明：

- `[` 表示包含左边界，`(` 表示不包含
- `]` 表示包含右边界，`)` 表示不包含
- `[:max]` 表示只有上界，`[min:]` 表示只有下界

约束在所有写入路径上一致生效，不会因值的来源或表示形式被绕过：

| 值的来源 / 形式 | 是否校验 |
|----------------|:---:|
| 配置中的原生数值（`port: 9090`） | ✅ |
| 类型不匹配、需字符串转换的值（`port: "9090"`、`json.Number`） | ✅ |
| 环境变量覆盖（`env=PORT`） | ✅ |
| 标签中声明的 `default` | ✅ |
| `time.Duration` 字段（比较单位为**纳秒**，与底层 `int64` 一致） | ✅ |
| `FillDefault` 填充的默认值 | ✅ |

```go
type Config struct {
    // 越界默认值属于标签声明错误，会在加载期直接报错
    Port    int           `json:",default=8080,range=[1:65535]"`
    // duration 的 range 以纳秒为单位：1ns ≤ timeout ≤ 10s
    Timeout time.Duration `json:",default=5s,range=[1:10000000000]"`
}
```

### 组合使用

多个选项可以组合使用：

```go
type Config struct {
    Port int    `json:"port,default=8080,range=[1:65535]"`
    Mode string `json:"mode,default=dev,options=[dev,prod,test],env=APP_MODE"`
}
```

## 支持的数据类型

| 类型 | 示例 |
| ------ | ------ |
| `string` | `` Host string `json:"host"` `` |
| `int/int8/.../int64` | `` Port int `json:"port"` `` |
| `uint/uint8/.../uint64` | `` Count uint `json:"count"` `` |
| `float32/float64` | `` Score float64 `json:"score"` `` |
| `bool` | `` Debug bool `json:"debug"` `` |
| `time.Duration` | `` Timeout time.Duration `json:"timeout"` `` |
| `[]string` / `[]int` 等 | `` Hosts []string `json:"hosts"` `` |
| `map[string]string` | `` Labels map[string]string `json:"labels"` `` |
| 指针类型 | `` Host *string `json:"host"` `` |
| 嵌套结构体 | 自动递归处理 |
| 匿名嵌入结构体 | 自动展开处理 |

## API

### UnmarshalJsonMap

使用默认的 `json` 标签将 map 反序列化到结构体：

```go
func UnmarshalJsonMap(m map[string]any, v any, opts ...UnmarshalOption) error
```

```go
var cfg Config
err := mapping.UnmarshalJsonMap(m, &cfg)
```

### UnmarshalKey

`UnmarshalJsonMap` 的简化版，不带额外选项：

```go
func UnmarshalKey(m map[string]any, v any) error
```

### NewDefaultUnmarshaler

创建仅填充默认值的反序列化器（等价于 `NewUnmarshaler("json", WithDefault())`），
是 conf、logger、orm、redisx、jwt、httpx、taskq、trace 等模块中 fillDefault
模式的统一入口：

```go
func NewDefaultUnmarshaler() *Unmarshaler
```

```go
u := mapping.NewDefaultUnmarshaler()
err := u.Unmarshal(map[string]any{}, &cfg)
```

### NewUnmarshaler

创建自定义配置的反序列化器：

```go
func NewUnmarshaler(key string, opts ...UnmarshalOption) *Unmarshaler
```

```go
u := mapping.NewUnmarshaler("json", mapping.WithDefault())
err := u.Unmarshal(map[string]any{}, &cfg)
```

### 配置选项

```go
// 仅填充默认值和环境变量（结构体必须为零值）
mapping.WithDefault()

// 从字符串模式解析所有值
mapping.WithStringValues()

// 键名规范化（如大小写不敏感匹配）
mapping.WithCanonicalKeyFunc(strings.ToLower)
```

### 仅填充默认值

`WithDefault()` 模式用于零值结构体填充默认配置，不读取任何 map 数据：

```go
type LoggerConfig struct {
    Level  string `json:",default=info"`
    Output string `json:",default=stdout"`
}

var cfg LoggerConfig
u := mapping.NewUnmarshaler("json", mapping.WithDefault())
err := u.Unmarshal(map[string]any{}, &cfg)
// cfg.Level = "info", cfg.Output = "stdout"
```

### FillAndOverride / MustFillAndOverride

先用标签默认值填充 `defaults`，再用 `overrides` 中的非零字段覆盖。
这是各模块 `fillDefault` 的统一替代方案，消除重复的逐字段覆盖代码。

```go
func FillAndOverride(defaults any, overrides any) error
func MustFillAndOverride(defaults any, overrides any) // 出错 panic
```

**覆盖规则**：

| 类型 | 覆盖条件 | 说明 |
| ------ | ------ | ------ |
| 指针类型 | 非 nil 即覆盖 | 支持 `*int=0`、`*string=""` 等显式零值 |
| 布尔类型 | `true` 覆盖 | `false` 视为未设置；需用 `*bool` 显式设 false |
| string | 非空覆盖 | 空字符串视为未设置；`optional` 且无 `default` 的 string 始终覆盖 |
| slice/map | 非 nil 且非空覆盖 | 空切片/map 视为未设置 |
| 嵌套结构体 | 递归覆盖子字段 | 保留默认值，只覆盖非零子字段 |
| 其他类型 | 非零值覆盖 | — |

> **`optional` 标签的特殊语义**：标为 `optional` 且无 `default` 的 string 类型字段（如 `Password`、`DSN`、`KeyPrefix`）会被视为"始终使用用户值"，即空字符串也会覆盖。这与各模块原有的手写 `fillDefault` 行为一致。

```go
type Config struct {
    Host     string        `json:",default=127.0.0.1"`
    Port     int           `json:",default=8080"`
    Password string        `json:",optional"`  // 空字符串也是有效值
    Timeout  time.Duration `json:",default=5s"`
}

var c Config
mapping.MustFillAndOverride(&c, Config{
    Host:     "0.0.0.0",   // 覆盖默认值
    Port:     9090,         // 覆盖默认值
    Password: "",            // 覆盖（optional 无 default，空字符串也生效）
    // Timeout 未设置，保留默认值 5s
})
```

### 大小写不敏感匹配

```go
type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

// 配置文件中的 "HOST"、"Host"、"host" 都能匹配
m := map[string]any{"HOST": "localhost", "PORT": json.Number("8080")}
err := mapping.UnmarshalJsonMap(m, &cfg, mapping.WithCanonicalKeyFunc(strings.ToLower))
```

## 在项目中的角色

`mapping` 是一个底层工具包，被以下包共用：

- **conf**：配置文件解析（JSON/YAML → map → 结构体）
- **logger**：填充日志默认配置（`MustFillAndOverride`）
- **orm**：填充数据库默认配置（`MustFillAndOverride`）
- **redisx**：填充 Redis 默认配置（`MustFillAndOverride`）
- **httpx**：填充 HTTP 服务器默认配置（`MustFillAndOverride`）
- **jwt**：填充 JWT 默认配置（`MustFillAndOverride`）
- **trace**：填充链路追踪默认配置
