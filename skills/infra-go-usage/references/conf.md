# conf

Configuration loading and parsing package; supports JSON / YAML formats and provides default-value filling, environment variable reading, parameter validation and more.

## Features

- **Multiple formats**: `.json`, `.yaml`, `.yml`
- **Defaults**: set field defaults through the `default` tag directive
- **Environment variables**: read a value from an environment variable first through the `env` tag directive
- **Parameter validation**:
  - `range` — numeric range validation
  - `options` — enum value validation
  - `optional` — mark a field as optional
- **Custom validation**: implement the `Validator` interface; it is called automatically after loading
- **Environment variable expansion**: config files may reference environment variables with `${VAR}` / `$VAR` and support the `${VAR:-default}` default-value syntax
- **Case-insensitive**: keys in the config file match struct field names case-insensitively (map data keys are left unchanged, see "Slices and Maps")
- **Nested structs**: supports nested structs, anonymous embedded fields, slices, maps and other complex types
- **Big-integer precision**: uses `json.Number` to preserve numeric precision, avoiding loss for big integers

## Quick start

### Installation

```bash
go get github.com/chihqiang/infra-go/conf
```

### Basic usage

Define a config struct and declare field names and options with `json` tags:

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
    // panics on error
    conf.MustLoad("config.json", &cfg)
    fmt.Printf("%+v\n", cfg)
}
```

`config.json`:

```json
{
    "host": "127.0.0.1",
    "port": 9090,
    "timeout": "5s",
    "maxConns": 50000,
    "logMode": "console"
}
```

The equivalent `config.yaml`:

```yaml
host: 127.0.0.1
port: 9090
timeout: 5s
maxConns: 50000
logMode: console
```

## API

### Load

Loads config from a file and returns an error.

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

Loads config from a file and panics on error. Suited to program startup.

```go
func MustLoad(path string, v any, opts ...Option)
```

```go
var cfg Config
conf.MustLoad("config.yaml", &cfg)
```

### LoadFromJSONBytes

Loads config from a JSON byte stream.

```go
func LoadFromJSONBytes(content []byte, v any) error
```

```go
var cfg Config
err := conf.LoadFromJSONBytes([]byte(`{"host": "127.0.0.1", "port": 9090}`), &cfg)
```

### LoadFromYAMLBytes

Loads config from a YAML byte stream.

```go
func LoadFromYAMLBytes(content []byte, v any) error
```

```go
var cfg Config
err := conf.LoadFromYAMLBytes([]byte("host: 127.0.0.1\nport: 9090\n"), &cfg)
```

### FillDefault

Only fills defaults and environment variables for a struct (no file loading). Requires every struct field to be at its zero value.

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

`Option` that expands environment variable references (`${VAR}` or `$VAR`) in the config file.
It also supports the `${VAR:-default}` syntax: when `VAR` is unset or an empty string, it falls back to `default` (a literal value).

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

```yaml
# config.yaml — secret falls back to dev-secret when JWT_SECRET is unset
jwt:
  secret: ${JWT_SECRET:-dev-secret}
  issuer: ${JWT_ISSUER:-my-app}
```

```go
var cfg ServerConfig
conf.MustLoad("config.yaml", &cfg, conf.UseEnv())
```

Note: the `:-` default follows shell semantics (the default is used when the variable is unset **or empty**),
and `default` is a literal value — any `${...}` inside it is not expanded a second time.

### ExpandEnv

A public environment-variable expansion function; `UseEnv` calls it internally (**after** parsing,
to expand the parsed strings — see "Expansion timing and safety" below). Call it directly if you need the
same expansion semantics outside config loading:

```go
func ExpandEnv(s string) string
```

```go
// expand arbitrary text (including ${VAR} / $VAR / ${VAR:-default} / $$ escapes)
os.Setenv("MODE", "prod")
conf.ExpandEnv("host=${DB_HOST:-localhost} mode=$MODE")
// -> "host=localhost mode=prod"

// $$ expands to a literal $ (for values containing $, such as passwords)
conf.ExpandEnv("p$$ssword")   // -> "p$ssword"
conf.ExpandEnv("100$$-${MODE}") // -> "100$-prod"
```

### Expansion timing and safety

`UseEnv` **parses first, then expands**: within the parsed data structure only strings (values and map keys)
are expanded, and the expansion result is never parsed again. Therefore:

- **The config structure is never broken**: even if an environment variable's value contains a fragment like
  `","admin":true`, it only becomes a string value and never creates new config keys out of thin air.
- **Special characters are safe**: quotes, braces, commas, newlines, `:` and so on in the value don't affect parsing.
- **A literal `$` can be written**: escape it as `$$` (the old text-replacement implementation swallowed
  `p$ssword` down to `p` when unescaped).
- **No double expansion**: a `${...}` inside an environment variable's value is kept as-is.
- Duplicate-key protection: if two keys expand to the same name an error is returned, rather than silently
  dropping one of them.

> **Note**: the value of an environment variable is always written as a **string**. If a config item needs an
> array or an object, write that structure directly in the config file instead of stuffing a JSON string into
> an environment variable — that approach relies on "parse again after expansion", which is exactly the source
> of the injection problem described above.

## Tag directives

All directives are declared in the `json` tag, separated by commas, in the form `json:"key,directive1,directive2,..."`.
When `key` is empty, the field name is used as the key.

### default — default values

Filled in when the config file doesn't provide that field.

```go
type Config struct {
    Host    string        `json:",default=0.0.0.0"`
    Port    int           `json:",default=8080"`
    Timeout time.Duration `json:",default=3s"`
    Hosts   []string      `json:",default=[a.com,b.com]"`
}
```

Supports all primitive types, `time.Duration` and slices. The slice default format is `[a,b,c]`.

### env — environment variables

Reads the value from the given environment variable first, falling back to the config file when it is empty.

```go
type Config struct {
    Name string `json:",env=APP_NAME"`
    Port int    `json:",default=8080"`
}
```

```bash
APP_NAME=myapp ./myapp
```

### optional — optional fields

Marks a field as optional so no error is raised when the config file doesn't provide it.

```go
type Config struct {
    Name    string `json:"name"`        // required
    Verbose bool   `json:",optional"`   // optional
}
```

### range — numeric range

Validates that a number falls within the given range, in the format `[left:right]`, with open and closed bounds.

```go
type Config struct {
    Port     int   `json:",range=[1:65535]"`      // 1 ≤ port ≤ 65535
    MaxConns int   `json:",range=[1:100000]"`     // 1 ≤ maxConns ≤ 100000
    Level    int64 `json:",range=[0:1000)"`       // 0 ≤ level < 1000
    Ratio    float64 `json:",range=(0:1]"`        // 0 < ratio ≤ 1
}
```

Range symbols:

| Symbol | Meaning |
| ------ | ------ |
| `[` | Closed bound: includes the left edge |
| `(` | Open bound: excludes the left edge |
| `]` | Closed bound: includes the right edge |
| `)` | Open bound: excludes the right edge |

Omitting a bound means unlimited: `[:100]` means ≤ 100 and `[1:]` means ≥ 1.

### options — enum values

Validates that the value is in the allowed list of options.

```go
type Config struct {
    LogMode string `json:",options=[file,console]"`
    Env     string `json:",options=[dev|staging|prod]"`  // a | separator also works
}
```

### string — parse from a string

Forces the config value to be treated as a string before parsing; useful for automatic conversion when the value type doesn't match.

```go
type Config struct {
    Port int `json:"port,string"`
}
```

```json
{"port": "9090"}  // the string "9090" is parsed into the int 9090
```

## Custom validation

Implement the `Validator` interface to run custom validation logic automatically once config loading completes.

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

// Validate() is called automatically when the config is loaded
var cfg ServerConfig
err := conf.Load("config.json", &cfg)
// if Port <= 1024, err contains "port must be > 1024"
```

## Nested structs

Arbitrary nesting levels are supported; defaults and validation are applied independently at every level.

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

After loading, `DB.Host` uses the default `localhost` and every `Redis` field uses its default.

## Anonymous embedded fields

Anonymous embedded structs are supported; the embedded fields are flattened.

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

After loading, `Server.Host` is `0.0.0.0` and `Server.Port` is `8080`.

## Slices and Maps

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

> **Map keys are kept as-is**: case-insensitive matching only applies to the “config key → struct field”
> lookup and never rewrites the data keys of a map field. So the keys of `labels` are preserved verbatim:
>
> ```json
> {"labels": {"AppName": "svc", "Env": "prod"}}
> ```
>
> After loading, `Labels` is `{"AppName": "svc", "Env": "prod"}` (not lower-cased).
> Keys that differ only in case (such as `AppName` and `appname`) are kept as two separate keys and are not merged.
>
> If several keys differing only in case at the same level try to match **the same field** (for example
> both `Host` and `HOST` with a field tag of `host`), an exact match wins; if none matches exactly and
> several candidates remain, an “ambiguous key” error is returned rather than picking one arbitrarily
> based on map iteration order.

## Big-integer precision

Internally `json.Number` preserves numeric precision, so big integers are not truncated.

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

## Complete example

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
    // reference an environment variable
    os.Setenv("DB_PASSWORD", "secret")

    var cfg ServerConfig
    conf.MustLoad("config.yaml", &cfg, conf.UseEnv())

    fmt.Printf("Server: %s:%d\n", cfg.Host, cfg.Port)
    fmt.Printf("DB: %s:%d\n", cfg.DB.Host, cfg.DB.Port)
}
```

`config.yaml`:

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
