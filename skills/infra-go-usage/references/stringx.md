# stringx

A general-purpose string utility package offering random string generation, string checks,
conversions, splitting/joining, and other practical helpers.

## Features

- **Random generation**: upper/lower/digit type selection, safe for concurrent use
- **String checks**: emptiness checks, blank detection, default fallback
- **Case conversion**: camelCase ↔ snake_case, first-letter case
- **String operations**: reverse, substring, split, join, repeat, chunk
- **Search and counting**: substring lookup and occurrence counting

## Installation

```bash
go get github.com/chihqiang/infra-go/stringx
```

## Quick start

```go
package main

import (
    "fmt"

    "github.com/chihqiang/infra-go/stringx"
)

func main() {
    // random strings
    fmt.Println(stringx.Rand())                     // "aB3kZ9xQ"
    fmt.Println(stringx.Randn(6, stringx.RandTypeUpper)) // "ABCDEF"
    fmt.Println(stringx.RandId())                   // "3f2a8b1c9d4e5f6a"

    // string checks
    fmt.Println(stringx.IsEmpty(""))                // true
    fmt.Println(stringx.IsNotBlank("  hello  "))    // true
    fmt.Println(stringx.DefaultIfBlank("", "N/A"))  // "N/A"

    // case conversion
    fmt.Println(stringx.ToSnakeCase("UserName"))    // "user_name"
    fmt.Println(stringx.Capitalize("hello"))        // "Hello"

    // string operations
    fmt.Println(stringx.Reverse("hello"))           // "olleh"
    fmt.Println(stringx.Substr("hello", 1, 3))     // "el"
    fmt.Println(stringx.Repeat("ab", 3))           // "ababab"

    // split and join
    fmt.Println(stringx.Split("a,,b,c", ','))       // ["a" "b" "c"]
    fmt.Println(stringx.Join('-', "a", "b", "c"))   // "a-b-c"
}
```

## API

### Random strings

| Function | Description |
| ------ | ------ |
| `Rand() string` | Generate a random string of the default length (8) |
| `Randn(n int, randType RandType) string` | Generate a random string of the given length and type |
| `RandId() string` | Generate a cryptographically secure 16-character random ID |
| `Seed(seed int64)` | Set the random seed |

#### Random types

| Type | Description |
| ------ | ------ |
| `RandTypeAll` | All: upper + lower + digits (default) |
| `RandTypeUpper` | Uppercase letters only |
| `RandTypeLower` | Lowercase letters only |
| `RandTypeDigit` | Digits only |

```go
stringx.Randn(10, stringx.RandTypeAll)     // "aB3kZ9xQ1y"
stringx.Randn(10, stringx.RandTypeUpper)   // "ABCDEFGHIJ"
stringx.Randn(10, stringx.RandTypeLower)   // "abcdefghij"
stringx.Randn(10, stringx.RandTypeDigit)   // "1234567890"
```

### String checks

| Function | Description |
| ------ | ------ |
| `IsEmpty(s string) bool` | Whether the string is empty |
| `IsNotBlank(s string) bool` | Whether the string is non-empty and not all whitespace |
| `DefaultIfBlank(s, def string) string` | Return the default when the string is empty or all whitespace |

### Case conversion

| Function | Description |
| ------ | ------ |
| `ToCamelCase(s string) string` | Lowercase the first letter |
| `ToSnakeCase(s string) string` | Convert camelCase to snake_case |
| `Capitalize(s string) string` | Uppercase the first letter |

```go
stringx.ToCamelCase("Hello")    // "hello"
stringx.ToCamelCase("Äbc")      // "äbc" (leading multi-byte rune handled correctly)
stringx.ToSnakeCase("HTTPServer") // "http_server"
stringx.Capitalize("hello")     // "Hello"
```

> `ToCamelCase` / `Capitalize` locate the first rune boundary with `utf8.DecodeRuneInString`,
> supporting multi-byte UTF-8 (Chinese, Latin Extended, emoji, and so on) without byte
> truncation. Invalid UTF-8 input is returned unchanged.

### String operations

| Function | Description |
| ------ | ------ |
| `Reverse(s string) string` | Reverse a string |
| `Substr(s string, start, end int) string` | Safe substring extraction with negative indices |
| `Repeat(s string, n int) string` | Repeat a string n times |
| `Chunk(s string, size int) []string` | Split into fixed-length chunks |

```go
stringx.Reverse("hello")              // "olleh"
stringx.Substr("hello", 1, 3)         // "el"
stringx.Substr("hello", -3, 5)        // "llo"
stringx.Repeat("ab", 3)              // "ababab"
stringx.Chunk("abcdef", 2)           // ["ab" "cd" "ef"]
```

> `Repeat` never panics: it returns an empty string when `n <= 0`, when `s` is empty, or when the
> result would exceed the 1 GiB limit. Concatenate manually if you need more than that.

### Split and join

| Function | Description |
| ------ | ------ |
| `Join(sep byte, elem ...string) string` | Join strings, skipping empty ones |
| `Split(s string, sep byte) []string` | Split a string, dropping empty parts |

```go
stringx.Join(',', "a", "", "b", "c")  // "a,b,c"
stringx.Split("a,,b,c", ',')          // ["a" "b" "c"]
```

### Search and counting

| Function | Description |
| ------ | ------ |
| `IndexOf(s, substr string) int` | Index of the first occurrence of substr, or -1 when not found |
| `Count(s, substr string) int` | Count the occurrences of substr |
