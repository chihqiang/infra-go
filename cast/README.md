# cast

类型安全转换工具包，从 `any` 安全转换为各种 Go 基本类型。

## 特性

- **全面类型支持**：int/uint/float/bool/string/time.Duration/time.Time
- **多源类型**：原生类型、string、json.Number、fmt.Stringer、[]byte 等
- **安全转换**：`ToXxxE` 系列返回 error，`ToXxx` 系列返回零值
- **切片转换**：`ToIntSlice`、`ToStringSlice`，支持逗号分隔字符串
- **泛型转换**：`To[T]` 一行搞定，类型安全
- **json.Number 支持**：与 `conf`/`mapping` 包无缝配合

## 安装

```bash
go get github.com/chihqiang/infra-go/cast
```

## 快速开始

```go
package main

import (
    "fmt"
    "time"

    "github.com/chihqiang/infra-go/cast"
)

func main() {
    // 基本转换
    fmt.Println(cast.ToInt("123"))        // 123
    fmt.Println(cast.ToString(456))       // "456"
    fmt.Println(cast.ToBool("true"))      // true
    fmt.Println(cast.ToFloat64("3.14"))   // 3.14
    fmt.Println(cast.ToDuration("5s"))    // 5s

    // 泛型转换
    n := cast.To[int]("42")               // 42
    d := cast.To[time.Duration]("100ms")  // 100ms

    // 切片转换
    fmt.Println(cast.ToIntSlice("1,2,3"))       // [1 2 3]
    fmt.Println(cast.ToStringSlice("a,b,c"))    // [a b c]

    // 安全转换（带 error）
    val, err := cast.ToIntE("abc")
    if err != nil {
        fmt.Println("convert failed:", err)
    }
}
```

## API

### 数值转换

| 函数 | 说明 |
| ------ | ------ |
| `ToInt(v any) int` | 转为 int |
| `ToInt64(v any) int64` | 转为 int64 |
| `ToUint(v any) uint` | 转为 uint |
| `ToUint64(v any) uint64` | 转为 uint64 |
| `ToFloat32(v any) float32` | 转为 float32 |
| `ToFloat64(v any) float64` | 转为 float64 |

### 字符串/布尔转换

| 函数 | 说明 |
| ------ | ------ |
| `ToString(v any) string` | 转为 string，支持 []byte、fmt.Stringer、error |
| `ToBool(v any) bool` | 转为 bool，支持 "true"/"1"/"T" 等多种格式 |

### 时间转换

| 函数 | 说明 |
| ------ | ------ |
| `ToDuration(v any) time.Duration` | 转为 Duration，数值按纳秒、字符串按 "5s" 解析 |
| `ToTime(v any) time.Time` | 转为 Time，支持 RFC3339 字符串和 Unix 时间戳 |

### 切片转换

| 函数 | 说明 |
| ------ | ------ |
| `ToIntSlice(v any) []int` | 转为 []int，支持逗号分隔字符串 |
| `ToStringSlice(v any) []string` | 转为 []string，支持逗号分隔字符串 |

### 泛型转换

| 函数 | 说明 |
| ------ | ------ |
| `To[T any](v any) T` | 泛型转换，支持所有基本类型和结构体（JSON） |

### 带 error 版本

每个 `ToXxx` 都有对应的 `ToXxxE` 版本，返回 `(value, error)`：

```go
val, err := cast.ToIntE("abc")
// err != nil, val == 0
```

## 支持的源类型

| 源类型 | 示例 |
| ------ | ------ |
| `int/int8/.../int64` | `cast.ToString(42)` → "42" |
| `uint/uint8/.../uint64` | `cast.ToInt(uint(42))` → 42 |
| `float32/float64` | `cast.ToInt(3.99)` → 3 |
| `bool` | `cast.ToInt(true)` → 1 |
| `string` | `cast.ToInt("42")` → 42 |
| `[]byte` | `cast.ToString([]byte("hi"))` → "hi" |
| `json.Number` | `cast.ToInt(json.Number("42"))` → 42 |
| `fmt.Stringer` | `cast.ToString(err)` → err.Error() |
| `nil` | 所有转换返回零值 |
| `time.Duration` | `cast.ToDuration(time.Second)` → 1s |
| `time.Time` | `cast.ToTime(time.Now())` → 原值 |

## 错误处理

转换失败时返回 `*ErrCastFailed` 错误，包含原始类型和目标类型信息：

```go
_, err := cast.ToIntE("abc")
// err: cast: failed to cast string to int

var e *cast.ErrCastFailed
if errors.As(err, &e) {
    fmt.Println(e.From) // "string"
    fmt.Println(e.To)   // "int"
}
```
