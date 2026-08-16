# stringx

常用字符串工具包，提供随机字符串生成、字符串判断、转换、拆分连接等实用函数。

## 特性

- **随机生成**：支持大小写、数字类型选择，线程安全
- **字符串判断**：空值检测、空白检测、默认值回退
- **命名转换**：驼峰 ↔ 蛇形互转、首字母大小写
- **字符串操作**：反转、截取、分割、连接、重复、分块
- **搜索统计**：子串查找、出现次数统计

## 安装

```bash
go get github.com/chihqiang/infra-go/stringx
```

## 快速开始

```go
package main

import (
    "fmt"

    "github.com/chihqiang/infra-go/stringx"
)

func main() {
    // 随机字符串
    fmt.Println(stringx.Rand())                     // "aB3kZ9xQ"
    fmt.Println(stringx.Randn(6, stringx.RandTypeUpper)) // "ABCDEF"
    fmt.Println(stringx.RandId())                   // "3f2a8b1c9d4e5f6a"

    // 字符串判断
    fmt.Println(stringx.IsEmpty(""))                // true
    fmt.Println(stringx.IsNotBlank("  hello  "))    // true
    fmt.Println(stringx.DefaultIfBlank("", "N/A"))  // "N/A"

    // 命名转换
    fmt.Println(stringx.ToSnakeCase("UserName"))    // "user_name"
    fmt.Println(stringx.Capitalize("hello"))        // "Hello"

    // 字符串操作
    fmt.Println(stringx.Reverse("hello"))           // "olleh"
    fmt.Println(stringx.Substr("hello", 1, 3))     // "el"
    fmt.Println(stringx.Repeat("ab", 3))           // "ababab"

    // 拆分与连接
    fmt.Println(stringx.Split("a,,b,c", ','))       // ["a" "b" "c"]
    fmt.Println(stringx.Join('-', "a", "b", "c"))   // "a-b-c"
}
```

## API

### 随机字符串

| 函数 | 说明 |
| ------ | ------ |
| `Rand() string` | 生成默认长度（8）随机字符串 |
| `Randn(n int, randType RandType) string` | 生成指定长度和类型的随机字符串 |
| `RandId() string` | 生成加密安全的 16 位随机 ID |
| `Seed(seed int64)` | 设置随机种子 |

#### 随机类型

| 类型 | 说明 |
| ------ | ------ |
| `RandTypeAll` | 全部：大小写 + 数字（默认） |
| `RandTypeUpper` | 仅大写字母 |
| `RandTypeLower` | 仅小写字母 |
| `RandTypeDigit` | 仅数字 |

```go
stringx.Randn(10, stringx.RandTypeAll)     // "aB3kZ9xQ1y"
stringx.Randn(10, stringx.RandTypeUpper)   // "ABCDEFGHIJ"
stringx.Randn(10, stringx.RandTypeLower)   // "abcdefghij"
stringx.Randn(10, stringx.RandTypeDigit)   // "1234567890"
```

### 字符串判断

| 函数 | 说明 |
| ------ | ------ |
| `IsEmpty(s string) bool` | 判断字符串是否为空 |
| `IsNotBlank(s string) bool` | 判断字符串是否非空且非纯空白 |
| `DefaultIfBlank(s, def string) string` | 若为空或纯空白，返回默认值 |

### 命名转换

| 函数 | 说明 |
| ------ | ------ |
| `ToCamelCase(s string) string` | 首字母转小写 |
| `ToSnakeCase(s string) string` | 驼峰转蛇形命名 |
| `Capitalize(s string) string` | 首字母转大写 |

```go
stringx.ToCamelCase("Hello")    // "hello"
stringx.ToSnakeCase("HTTPServer") // "http_server"
stringx.Capitalize("hello")     // "Hello"
```

### 字符串操作

| 函数 | 说明 |
| ------ | ------ |
| `Reverse(s string) string` | 反转字符串 |
| `Substr(s string, start, end int) string` | 安全截取子串，支持负数索引 |
| `Repeat(s string, n int) string` | 重复字符串 n 次 |
| `Chunk(s string, size int) []string` | 按固定长度分块 |

```go
stringx.Reverse("hello")              // "olleh"
stringx.Substr("hello", 1, 3)         // "el"
stringx.Substr("hello", -3, 5)        // "llo"
stringx.Repeat("ab", 3)              // "ababab"
stringx.Chunk("abcdef", 2)           // ["ab" "cd" "ef"]
```

### 拆分与连接

| 函数 | 说明 |
| ------ | ------ |
| `Join(sep byte, elem ...string) string` | 连接字符串，跳过空串 |
| `Split(s string, sep byte) []string` | 拆分字符串，自动去除空串 |

```go
stringx.Join(',', "a", "", "b", "c")  // "a,b,c"
stringx.Split("a,,b,c", ',')          // ["a" "b" "c"]
```

### 搜索统计

| 函数 | 说明 |
| ------ | ------ |
| `IndexOf(s, substr string) int` | 返回子串首次出现的位置，未找到返回 -1 |
| `Count(s, substr string) int` | 计算子串出现次数 |

## 目录结构

```text
stringx/
├── random.go         — 随机字符串生成（Rand/Randn/RandId）
├── strings.go        — 字符串工具函数
├── random_test.go    — 随机生成单元测试
└── strings_test.go   — 字符串工具单元测试
```
