# hash

常用哈希算法封装包，提供 MD5、SHA1、SHA256、SHA512、SHA3、HMAC、Bcrypt 等哈希方法，统一返回十六进制字符串，无需手动处理编码。

## 特性

- **基础哈希**：MD5、SHA1、SHA224、SHA256、SHA384、SHA512、SHA512/224、SHA512/256
- **SHA3 系列**：SHA3-256、SHA3-512
- **HMAC**：HMAC-SHA1、HMAC-SHA256、HMAC-SHA512、HMAC-SHA3-256、HMAC-SHA3-512
- **密码哈希**：Bcrypt（带成本参数，支持验证、格式检测）
- **文件哈希**：大文件流式计算 MD5、SHA1、SHA256、SHA512
- **安全比较**：恒定时间比较，防止时序攻击
- **统一输出**：所有函数返回十六进制字符串，无需手动 `hex.EncodeToString`

## 安装

```bash
go get github.com/chihqiang/infra-go/hash
```

## 快速开始

```go
package main

import (
    "fmt"

    "github.com/chihqiang/infra-go/hash"
)

func main() {
    // 基础哈希
    fmt.Println(hash.MD5String("hello"))
    // 输出: 5d41402abc4b2a76b9719d911017c592

    fmt.Println(hash.SHA256String("hello"))
    // 输出: 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824

    // HMAC
    fmt.Println(hash.HMACSHA256String("secret-key", "hello world"))

    // 密码哈希
    hashed, _ := hash.BcryptHashDefault("myPassword123")
    fmt.Println(hash.BcryptMatch(hashed, "myPassword123")) // true
}
```

## API

### 基础哈希

每个算法提供 `[]byte` 和 `string` 两个版本：

| 函数 | 输入 | 输出长度 |
| ------ | ------ | ------ |
| `MD5` / `MD5String` | `[]byte` / `string` | 32 字符 |
| `SHA1` / `SHA1String` | `[]byte` / `string` | 40 字符 |
| `SHA224` / `SHA224String` | `[]byte` / `string` | 56 字符 |
| `SHA256` / `SHA256String` | `[]byte` / `string` | 64 字符 |
| `SHA384` / `SHA384String` | `[]byte` / `string` | 96 字符 |
| `SHA512` / `SHA512String` | `[]byte` / `string` | 128 字符 |
| `SHA512_224` / `SHA512_224String` | `[]byte` / `string` | 56 字符 |
| `SHA512_256` / `SHA512_256String` | `[]byte` / `string` | 64 字符 |
| `SHA3_256` / `SHA3_256String` | `[]byte` / `string` | 64 字符 |
| `SHA3_512` / `SHA3_512String` | `[]byte` / `string` | 128 字符 |

```go
// []byte 版本
hash.MD5([]byte("hello"))

// string 版本
hash.MD5String("hello")

// 通用哈希（自定义算法）
hash.Hash([]byte("hello"), sha256.New())
```

### 文件哈希

支持大文件流式计算，不会一次性加载到内存：

```go
md5sum, err := hash.FileMD5("/path/to/file")
sha1sum, err := hash.FileSHA1("/path/to/file")
sha256sum, err := hash.FileSHA256("/path/to/file")
sha512sum, err := hash.FileSHA512("/path/to/file")
```

### HMAC

| 函数 | 算法 |
| ------ | ------ |
| `HMACSHA1` / `HMACSHA1String` | HMAC-SHA1 |
| `HMACSHA256` / `HMACSHA256String` | HMAC-SHA256 |
| `HMACSHA512` / `HMACSHA512String` | HMAC-SHA512 |
| `HMACSHA3_256` | HMAC-SHA3-256 |
| `HMACSHA3_512` | HMAC-SHA3-512 |
| `HMAC` / `HMACHex` | 通用 HMAC（自定义算法） |

```go
// 使用字符串密钥
sig := hash.HMACSHA256String("secret-key", "hello world")

// 使用字节数组密钥
sig := hash.HMACSHA256([]byte("secret-key"), []byte("hello world"))

// 通用 HMAC
sig := hash.HMACHex(sha256.New, []byte("key"), []byte("data"))
```

### Bcrypt 密码哈希

Bcrypt 是专为密码设计的哈希算法，自带盐值，抗彩虹表攻击：

```go
// 哈希密码（使用默认成本 10）
hashed, err := hash.BcryptHashDefault("myPassword123")

// 哈希密码（自定义成本，4~31，推荐 10 或 12）
hashed, err := hash.BcryptHash("myPassword123", 12)

// 验证密码
ok := hash.BcryptMatch(hashed, "myPassword123")  // true
ok = hash.BcryptMatch(hashed, "wrongPassword")   // false

// 验证密码（返回 error）
err := hash.BcryptCompare(hashed, "myPassword123") // nil = 匹配

// 检查字符串是否为 bcrypt 哈希
hash.BcryptIsHashed("$2a$10$abc...") // true
```

**成本参数说明**：

| 常量 | 值 | 说明 |
| ------ | ------ | ------ |
| `BcryptCostMin` | 4 | 最小成本（最快，安全性最低） |
| `BcryptCostDefault` | 10 | 默认成本 |
| `BcryptCostMax` | 31 | 最大成本（最慢，安全性最高） |

### 安全比较

使用恒定时间比较，防止时序攻击：

```go
// 比较字节数组
hash.Equal([]byte("hash1"), []byte("hash2"))

// 比较十六进制字符串
hash.EqualHex("aaf4c61d...", "aaf4c61d...")
```

### 编码辅助

```go
// 编码
hexStr := hash.HexEncode([]byte("hello")) // "68656c6c6f"

// 解码
data, err := hash.HexDecode("68656c6c6f") // []byte("hello")
```

## 完整示例

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/chihqiang/infra-go/hash"
)

func main() {
    // --- 基础哈希 ---
    fmt.Println("=== 基础哈希 ===")
    fmt.Println("MD5:    ", hash.MD5String("hello"))
    fmt.Println("SHA1:   ", hash.SHA1String("hello"))
    fmt.Println("SHA256: ", hash.SHA256String("hello"))
    fmt.Println("SHA512: ", hash.SHA512String("hello"))
    fmt.Println("SHA3:   ", hash.SHA3_256String("hello"))

    // --- HMAC ---
    fmt.Println("\n=== HMAC ===")
    fmt.Println("HMAC-SHA256:", hash.HMACSHA256String("secret", "hello world"))

    // --- Bcrypt 密码哈希 ---
    fmt.Println("\n=== Bcrypt ===")
    password := "mySecretPassword"
    hashed, err := hash.BcryptHashDefault(password)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Hashed:", hashed)
    fmt.Println("Match: ", hash.BcryptMatch(hashed, password))

    // --- 文件哈希 ---
    fmt.Println("\n=== 文件哈希 ===")
    tmpFile := "/tmp/test.txt"
    os.WriteFile(tmpFile, []byte("hello world"), 0o644)

    fileMD5, err := hash.FileMD5(tmpFile)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("File MD5:", fileMD5)
    fmt.Println("Match:   ", hash.EqualHex(fileMD5, hash.MD5String("hello world")))
}
```

## 安全建议

- **密码存储**：使用 `BcryptHashDefault`，不要用 MD5/SHA256 存密码
- **MD5/SHA1**：已不安全，仅用于数据校验、去重等非安全场景
- **HMAC**：用于 API 签名、Webhook 验签等场景
- **Bcrypt 成本**：生产环境建议 12 或更高
- **哈希比较**：始终使用 `Equal` / `EqualHex`，不要用 `==`
