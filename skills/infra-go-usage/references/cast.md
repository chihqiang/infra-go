# cast

Type-safe conversion toolkit, converting from `any` to the various Go primitive types safely.

## Features

- **Comprehensive type support**: int/uint/float/bool/string/time.Duration/time.Time
- **Many source types**: native types, string, json.Number, fmt.Stringer, []byte, etc.
- **Safe conversion**: the `ToXxxE` family returns an error, the `ToXxx` family returns the zero value
- **Pointer conversion**: the `ToXxxPtr` family (nil on failure) plus the generic `Ptr`/`Val` (take address / safe dereference)
- **Slice conversion**: `ToIntSlice`, `ToStringSlice`, with comma-separated string support
- **Generic conversion**: `To[T]` in one line, type safe
- **json.Number support**: works seamlessly with the `conf`/`mapping` packages

## Installation

```bash
go get github.com/chihqiang/infra-go/cast
```

## Quick start

```go
package main

import (
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/cast"
)

func main() {
    // basic conversions
    fmt.Println(cast.ToInt("123"))        // 123
    fmt.Println(cast.ToString(456))       // "456"
    fmt.Println(cast.ToBool("true"))      // true
    fmt.Println(cast.ToFloat64("3.14"))   // 3.14
    fmt.Println(cast.ToDuration("5s"))    // 5s

    // generic conversions
    n := cast.To[int]("42")               // 42
    d := cast.To[time.Duration]("100ms")  // 100ms

    // slice conversions
    fmt.Println(cast.ToIntSlice("1,2,3"))       // [1 2 3]
    fmt.Println(cast.ToStringSlice("a,b,c"))    // [a b c]

    // safe conversion (with error)
    val, err := cast.ToIntE("abc")
    if err != nil {
        fmt.Println("convert failed:", err)
    }
}
```

## API

### Numeric conversion

| Function | Description |
| ------ | ------ |
| `ToInt(v any) int` | Convert to int |
| `ToInt64(v any) int64` | Convert to int64 |
| `ToUint(v any) uint` | Convert to uint |
| `ToUint64(v any) uint64` | Convert to uint64 |
| `ToFloat32(v any) float32` | Convert to float32 |
| `ToFloat64(v any) float64` | Convert to float64 |

### String/boolean conversion

| Function | Description |
| ------ | ------ |
| `ToString(v any) string` | Convert to string; supports []byte, fmt.Stringer, error |
| `ToBool(v any) bool` | Convert to bool; accepts "true"/"1"/"T" and other forms |

### Time conversion

| Function | Description |
| ------ | ------ |
| `ToDuration(v any) time.Duration` | Convert to Duration; numbers are treated as nanoseconds, strings parsed as "5s" |
| `ToTime(v any) time.Time` | Convert to Time; supports RFC3339 strings and Unix timestamps |

### Slice conversion

| Function | Description |
| ------ | ------ |
| `ToIntSlice(v any) []int` | Convert to []int; supports comma-separated strings |
| `ToStringSlice(v any) []string` | Convert to []string; supports comma-separated strings |

### Generic conversion

| Function | Description |
| ------ | ------ |
| `To[T any](v any) T` | Generic conversion; supports all primitive types and structs (JSON); returns the zero value on failure |
| `ToE[T any](v any) (T, error)` | Error-returning version of the generic conversion; returns the zero value and an error on failure, handy for falling back to a default |

```go
// ToE makes it easy to tell whether the conversion succeeded
v, err := cast.ToE[int]("123")
if err != nil {
    v = 0 // fall back to a default
}
```

### Pointer conversion

| Function | Description |
| ------ | ------ |
| `Ptr[T any](v T) *T` | Take the address of any value/literal, handy for assigning constants to pointer fields |
| `Val[T any](p *T, def ...T) T` | Safe dereference; returns the default (or zero) value when p is nil |
| `ToIntPtr/ToInt64Ptr/ToUintPtr/ToUint64Ptr(v any) *int/*int64/*uint/*uint64` | any → numeric pointer, nil on failure |
| `ToFloat32Ptr/ToFloat64Ptr(v any) *float32/*float64` | any → float pointer, nil on failure |
| `ToStringPtr(v any) *string` | any → string pointer, nil on failure |
| `ToBoolPtr(v any) *bool` | any → bool pointer, nil on failure |
| `ToDurationPtr/ToTimePtr(v any) *time.Duration/*time.Time` | any → time pointer, nil on failure |

```go
// take address / dereference
name := cast.Ptr("default")        // *string
s := cast.Val(name)                 // "default"
s2 := cast.Val(nilString, "fb")    // "fb" when nil

// convert to a pointer; nil on failure, ready for optional pointer fields
limit := cast.ToIntPtr("10")       // *int(10)
if limit == nil {
    // conversion failed
}
```

### Error-returning variants

Every `ToXxx` has a corresponding `ToXxxE` that returns `(value, error)`:

```go
val, err := cast.ToIntE("abc")
// err != nil, val == 0
```

## Supported source types

| Source type | Example |
| ------ | ------ |
| `int/int8/.../int64` | `cast.ToString(42)` → "42" |
| `uint/uint8/.../uint64` | `cast.ToInt(uint(42))` → 42 |
| `float32/float64` | `cast.ToInt(3.99)` → 3 |
| `bool` | `cast.ToInt(true)` → 1 |
| `string` | `cast.ToInt("42")` → 42 |
| `[]byte` | `cast.ToString([]byte("hi"))` → "hi" |
| `json.Number` | `cast.ToInt(json.Number("42"))` → 42 |
| `fmt.Stringer` | `cast.ToString(err)` → err.Error() |
| `nil` | all conversions return the zero value |
| `time.Duration` | `cast.ToDuration(time.Second)` → 1s |
| `time.Time` | `cast.ToTime(time.Now())` → the original value |

## Error handling

A failed conversion returns a `*ErrCastFailed` error carrying the source and target type information:

```go
_, err := cast.ToIntE("abc")
// err: cast: failed to cast string to int

var e *cast.ErrCastFailed
if errors.As(err, &e) {
    fmt.Println(e.From) // "string"
    fmt.Println(e.To)   // "int"
}
```
