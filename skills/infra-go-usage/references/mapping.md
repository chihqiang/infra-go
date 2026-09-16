# mapping

A struct tag parsing and `map[string]any` deserialization toolkit.

It supports defining defaults, environment variables, optional fields, enum validation and range validation through struct tags, and is the underlying foundation that `conf` and other packages use to fill in default configuration.

## Features

- **Defaults**: the `default=xxx` tag fills the value in when none is provided
- **Environment variables**: the `env=VAR_NAME` tag reads from the environment first
- **Optional fields**: the `optional` tag skips the field instead of erroring when no value is provided
- **Enum validation**: the `options=[a,b,c]` tag restricts the allowed field values
- **Range validation**: the `range=[0:65535]` tag validates numeric ranges
- **String mode**: the `string` tag forces the value to be parsed from a string
- **Case-insensitive**: the `WithCanonicalKeyFunc` option enables case-insensitive matching of config keys
- **Defaults only**: the `WithDefault()` option fills default config into a zero-value struct
- **Fill and override**: `FillAndOverride` / `MustFillAndOverride` fill defaults first and then override them with the user config's non-zero fields, removing the duplicated `fillDefault` code in each module
- **Rich type support**: primitives, `time.Duration`, slices, maps, pointers, nested structs, anonymous embedded structs

## Installation

```bash
go get github.com/chihqiang/infra-go/mapping
```

## Quick start

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

## Struct tags

All tag options are comma-separated inside the `json` tag value (the first segment is the field name, the following segments are options).

### Tag format

```text
`json:"<field name>,<option1>,<option2>,..."`
```

If you don't need to specify a field name (using the struct field name instead), you can start with a comma:

```text
`json:",default=localhost"`
```

### Supported tag options

| Option | Format | Description |
| ------ | ------ | ------ |
| `default` | `default=<value>` | Default value, filled in when none is provided |
| `env` | `env=<var name>` | Environment variable name; read from the environment first |
| `optional` | `optional` / `optional=dep` / `optional=!dep` | Field is optional; with a dependency it is conditionally optional |
| `options` | `options=[a,b,c]` or `options=a\|b\|c` | Enum validation; the value must be in the list |
| `range` | `range=[min:max]` | Numeric range validation |
| `string` | `string` | Force the value to be parsed in string mode |

> **Unknown options are an error**. A typo in a tag (such as `optinal`) or a mis-written prefix
> (such as `defaultFoo=bar`) returns an `unknown option` error instead of being silently ignored.
> That way "a misspelled tag makes the field behave unexpectedly" is caught at load time rather
> than surfacing later at runtime as `field not set`.
>
> Historically `inherit` was parsed but never took effect; it now errors explicitly (this design has
> no notion of a "parent"): if you genuinely need inheritance semantics, write the value out
> explicitly or give the field a `default`.

### Defaults (default)

```go
type Config struct {
    Host string `json:"host,default=localhost"`
    Port int    `json:"port,default=8080"`
}
```

Slice defaults are supported (JSON array or bracketed list):

```go
type Config struct {
    Hosts []string `json:"hosts,default=[a.com,b.com]"`
}
```

### Environment variables (env)

Environment variables take precedence over defaults and over values in the config file:

```go
type Config struct {
    Name string `json:"name,env=APP_NAME"`
}
// if the APP_NAME environment variable is set, its value is used
```

### Optional fields (optional)

A field without `optional` that receives no value is an error. Once marked, a missing value is skipped:

```go
type Config struct {
    Name string `json:"name"`           // required
    Port int    `json:",optional"`      // optional; zero value when not provided
}
```

#### Conditional optionality (optional=dep)

You can declare that a field is optional only when some dependency is set; the dependency name is a **config key** (that is, the json/yaml tag key of the dependency field):

```go
type Config struct {
    // optional when the dependency exists: with use_proxy given, proxy_url may be omitted
    UseProxy bool   `json:"use_proxy,optional"`
    ProxyURL string `json:"proxy_url,optional=use_proxy"`

    // optional when the dependency does not exist: without mode, custom_mode need not be provided
    Mode       string `json:"mode,optional"`
    CustomMode string `json:"custom_mode,optional=!mode"`
}
```

Semantics and override rules:

| Tag | Optional when | When the condition is not met |
|------|---------|-------------|
| `optional=dep` | `dep` **is set** | a missing value is an error (treated as required) |
| `optional=!dep` | `dep` **is not set** | a missing value is an error (treated as required) |

- The dependency name must match a field key in the same struct (including anonymous embedded fields),
  otherwise a `does not match any field key` error is returned — this prevents a misspelled dependency name
  from silently disabling conditional optionality.
- Dependency lookup and field values follow the same path: with `WithCanonicalKeyFunc(strings.ToLower)`
  configured, it is case-insensitive as well.
- When a field also has a `default`, **the dependency check is skipped**: a default always provides a value,
  so the dependency is irrelevant.
- When the field provides a value itself, no dependency is evaluated.

### Enum validation (options)

```go
type Config struct {
    Mode string `json:"mode,options=[dev,prod,test]"`
    // or pipe-separated: json:"mode,options=dev|prod|test"
}
// an error is returned when the value is not in the list
```

### Range validation (range)

Closed ranges `[min:max]`, open ranges `(min:max)` and mixtures are supported:

```go
type Config struct {
    Port    int     `json:"port,range=[1:65535]"`       // 1 ≤ port ≤ 65535
    Score   float64 `json:"score,range=[0:100)"`        // 0 ≤ score < 100
    Age     int     `json:"age,range=(0:150]"`          // 0 < age ≤ 150
    Temperature float64 `json:"temp,range=[:100]"`      // temp ≤ 100
    Discount float64 `json:"discount,range=[0:]"`       // discount ≥ 0
}
```

Format notes:

- `[` includes the left edge, `(` excludes it
- `]` includes the right edge, `)` excludes it
- `[:max]` means only an upper bound, `[min:]` only a lower bound

Constraints apply consistently on every write path and cannot be bypassed by the source or representation of a value:

| Value source / form | Validated |
|----------------|:---:|
| Native numbers in the config (`port: 9090`) | ✅ |
| Values needing string conversion on a type mismatch (`port: "9090"`, `json.Number`) | ✅ |
| Environment variable overrides (`env=PORT`) | ✅ |
| A `default` declared in the tag | ✅ |
| `time.Duration` fields (compared in **nanoseconds**, matching the underlying `int64`) | ✅ |
| Defaults filled by `FillDefault` | ✅ |

```go
type Config struct {
    // an out-of-range default is a tag declaration error and fails immediately at load time
    Port    int           `json:",default=8080,range=[1:65535]"`
    // a duration range is in nanoseconds: 1ns ≤ timeout ≤ 10s
    Timeout time.Duration `json:",default=5s,range=[1:10000000000]"`
}
```

### Combining options

Several options can be combined:

```go
type Config struct {
    Port int    `json:"port,default=8080,range=[1:65535]"`
    Mode string `json:"mode,default=dev,options=[dev,prod,test],env=APP_MODE"`
}
```

## Supported data types

| Type | Example |
| ------ | ------ |
| `string` | `` Host string `json:"host"` `` |
| `int/int8/.../int64` | `` Port int `json:"port"` `` |
| `uint/uint8/.../uint64` | `` Count uint `json:"count"` `` |
| `float32/float64` | `` Score float64 `json:"score"` `` |
| `bool` | `` Debug bool `json:"debug"` `` |
| `time.Duration` | `` Timeout time.Duration `json:"timeout"` `` |
| `[]string` / `[]int` etc. | `` Hosts []string `json:"hosts"` `` |
| `map[string]string` | `` Labels map[string]string `json:"labels"` `` |
| Pointer types | `` Host *string `json:"host"` `` |
| Nested structs | handled recursively |
| Anonymous embedded structs | flattened automatically |

## API

### UnmarshalJsonMap

Deserializes a map into a struct using the default `json` tag:

```go
func UnmarshalJsonMap(m map[string]any, v any, opts ...UnmarshalOption) error
```

```go
var cfg Config
err := mapping.UnmarshalJsonMap(m, &cfg)
```

### UnmarshalKey

A simplified version of `UnmarshalJsonMap` without extra options:

```go
func UnmarshalKey(m map[string]any, v any) error
```

### NewDefaultUnmarshaler

Creates an unmarshaler that only fills defaults (equivalent to `NewUnmarshaler("json", WithDefault())`).
It is the single entry point for the fillDefault pattern used by the conf, logger, orm, redisx, jwt,
httpx, taskq and trace modules:

```go
func NewDefaultUnmarshaler() *Unmarshaler
```

```go
u := mapping.NewDefaultUnmarshaler()
err := u.Unmarshal(map[string]any{}, &cfg)
```

### NewUnmarshaler

Creates an unmarshaler with a custom configuration:

```go
func NewUnmarshaler(key string, opts ...UnmarshalOption) *Unmarshaler
```

```go
u := mapping.NewUnmarshaler("json", mapping.WithDefault())
err := u.Unmarshal(map[string]any{}, &cfg)
```

### Configuration options

```go
// fill only defaults and environment variables (the struct must be zero-valued)
mapping.WithDefault()

// parse all values in string mode
mapping.WithStringValues()

// key normalization (e.g. case-insensitive matching)
mapping.WithCanonicalKeyFunc(strings.ToLower)
```

### Filling defaults only

The `WithDefault()` mode fills default config into a zero-value struct and reads no map data at all:

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

First fills `defaults` with the tag defaults, then overrides them with the non-zero fields of `overrides`.
This is the unified replacement for each module's `fillDefault` and removes the duplicated per-field override code.

```go
func FillAndOverride(defaults any, overrides any) error
func MustFillAndOverride(defaults any, overrides any) // panics on error
```

**Override rules**:

| Type | Override condition | Description |
| ------ | ------ | ------ |
| Pointer types | overridden whenever non-nil | supports explicit zero values such as `*int=0`, `*string=""` |
| Boolean | overridden when `true` | `false` counts as unset; use `*bool` to set false explicitly |
| string | overridden when non-empty | an empty string counts as unset; an `optional` string without `default` is always overridden |
| slice/map | overridden when non-nil and non-empty | an empty slice/map counts as unset |
| Nested structs | recursively overrides sub-fields | defaults are kept and only non-zero sub-fields are overridden |
| Other types | overridden when non-zero | — |

> **Special semantics of the `optional` tag**: a string field marked `optional` without a `default`
> (such as `Password`, `DSN`, `KeyPrefix`) is treated as "always use the user's value", i.e. an empty
> string overrides as well. This matches the hand-written `fillDefault` behavior of the modules.

```go
type Config struct {
    Host     string        `json:",default=127.0.0.1"`
    Port     int           `json:",default=8080"`
    Password string        `json:",optional"`  // an empty string is a valid value too
    Timeout  time.Duration `json:",default=5s"`
}

var c Config
mapping.MustFillAndOverride(&c, Config{
    Host:     "0.0.0.0",   // overrides the default
    Port:     9090,         // overrides the default
    Password: "",            // overrides (optional without default: empty string still counts)
    // Timeout not set, so the default 5s is kept
})
```

### Case-insensitive matching

```go
type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

// "HOST", "Host" and "host" in the config file all match
m := map[string]any{"HOST": "localhost", "PORT": json.Number("8080")}
err := mapping.UnmarshalJsonMap(m, &cfg, mapping.WithCanonicalKeyFunc(strings.ToLower))
```

## Role in the project

`mapping` is a low-level toolkit shared by the following packages:

- **conf**: config file parsing (JSON/YAML → map → struct)
- **logger**: fills the logging defaults (`MustFillAndOverride`)
- **orm**: fills the database defaults (`MustFillAndOverride`)
- **redisx**: fills the Redis defaults (`MustFillAndOverride`)
- **httpx**: fills the HTTP server defaults (`MustFillAndOverride`)
- **jwt**: fills the JWT defaults (`MustFillAndOverride`)
- **trace**: fills the tracing defaults
