# hash

A package wrapping common hashing algorithms: MD5, SHA1, SHA256, SHA512, SHA3, HMAC, Bcrypt and more. Basic/file hashes all return a hexadecimal string; AES-GCM and `HMACSign` return base64; Bcrypt returns a `$2a$`-format hash — no manual encoding needed in any case.

## Features

- **Basic hashing**: MD5, SHA1, SHA224, SHA256, SHA384, SHA512, SHA512/224, SHA512/256
- **SHA3 family**: SHA3-256, SHA3-512
- **HMAC**: HMAC-SHA1, HMAC-SHA256, HMAC-SHA512, HMAC-SHA3-256, HMAC-SHA3-512
- **AES-GCM encryption**: authenticated encryption (AEAD), guaranteeing both confidentiality and integrity
- **HMAC sign/verify**: `HMACSign` / `HMACVerify`, base64-encoded, constant-time comparison
- **Password hashing**: Bcrypt (with cost parameter, supports verification and format detection)
- **File hashing**: streaming MD5, SHA1, SHA256, SHA512 for large files
- **Secure comparison**: constant-time comparison to prevent timing attacks
- **Output convention**: basic/file hashes return hexadecimal strings; AES-GCM and `HMACSign` return base64; Bcrypt returns a `$2a$` hash — no manual encoding needed in any case

## Installation

```bash
go get github.com/chihqiang/infra-go/hash
```

## Quick start

```go
package main

import (
    "fmt"

    "github.com/chihqiang/infra-go/hash"
)

func main() {
    // basic hashing
    fmt.Println(hash.MD5String("hello"))
    // output: 5d41402abc4b2a76b9719d911017c592

    fmt.Println(hash.SHA256String("hello"))
    // output: 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824

    // HMAC
    fmt.Println(hash.HMACSHA256String("secret-key", "hello world"))

    // password hashing
    hashed, _ := hash.BcryptHashDefault("myPassword123")
    fmt.Println(hash.BcryptMatch(hashed, "myPassword123")) // true
}
```

## API

### Basic hashing

Each algorithm comes in both a `[]byte` and a `string` version:

| Function | Input | Output length |
| ------ | ------ | ------ |
| `MD5` / `MD5String` | `[]byte` / `string` | 32 chars |
| `SHA1` / `SHA1String` | `[]byte` / `string` | 40 chars |
| `SHA224` / `SHA224String` | `[]byte` / `string` | 56 chars |
| `SHA256` / `SHA256String` | `[]byte` / `string` | 64 chars |
| `SHA384` / `SHA384String` | `[]byte` / `string` | 96 chars |
| `SHA512` / `SHA512String` | `[]byte` / `string` | 128 chars |
| `SHA512_224` / `SHA512_224String` | `[]byte` / `string` | 56 chars |
| `SHA512_256` / `SHA512_256String` | `[]byte` / `string` | 64 chars |
| `SHA3_256` / `SHA3_256String` | `[]byte` / `string` | 64 chars |
| `SHA3_512` / `SHA3_512String` | `[]byte` / `string` | 128 chars |

```go
// []byte version
hash.MD5([]byte("hello"))

// string version
hash.MD5String("hello")

// generic hash (custom algorithm)
hash.Hash([]byte("hello"), sha256.New())
```

### File hashing

Supports streaming computation for large files, never loading the whole file into memory:

```go
md5sum, err := hash.FileMD5("/path/to/file")
sha1sum, err := hash.FileSHA1("/path/to/file")
sha256sum, err := hash.FileSHA256("/path/to/file")
sha512sum, err := hash.FileSHA512("/path/to/file")
```

### HMAC

| Function | Algorithm |
| ------ | ------ |
| `HMACSHA1` / `HMACSHA1String` | HMAC-SHA1 |
| `HMACSHA256` / `HMACSHA256String` | HMAC-SHA256 |
| `HMACSHA512` / `HMACSHA512String` | HMAC-SHA512 |
| `HMACSHA3_256` | HMAC-SHA3-256 |
| `HMACSHA3_512` | HMAC-SHA3-512 |
| `HMAC` / `HMACHex` | generic HMAC (custom algorithm) |

```go
// string key
sig := hash.HMACSHA256String("secret-key", "hello world")

// byte-slice key
sig := hash.HMACSHA256([]byte("secret-key"), []byte("hello world"))

// generic HMAC
sig := hash.HMACHex(sha256.New, []byte("key"), []byte("data"))
```

### AES-GCM encryption

AES-GCM is authenticated encryption (AEAD) guaranteeing both confidentiality and integrity (tamper resistance); the nonce is randomly generated each time. It returns the base64-encoded `nonce || ciphertext`:

```go
// the key must be 16/24/32 bytes (AES-128/192/256)
key := []byte("0123456789abcdef")

// encrypt: returns the base64-encoded ciphertext
encrypted, err := hash.AESGCMEncrypt(key, []byte("hello"))

// decrypt: returns an error on a wrong key or tampered data
decrypted, err := hash.AESGCMDecrypt(key, encrypted)
```

### HMAC sign/verify

`HMACSign` / `HMACVerify` are used for request signing and tamper detection (e.g. `httpx.WithContentSecurity`). The signature is base64-encoded and verification uses constant-time comparison (`hmac.Equal`) to prevent timing attacks:

```go
// sign: returns the base64-encoded HMAC-SHA256
sig := hash.HMACSign(key, "timestamp\nmethod\npath")

// verify: returns whether it matches
ok := hash.HMACVerify(key, "timestamp\nmethod\npath", sig)
```

### Bcrypt password hashing

Bcrypt is a hash algorithm designed specifically for passwords; it includes its own salt and resists rainbow-table attacks:

```go
// hash a password (default cost 10)
hashed, err := hash.BcryptHashDefault("myPassword123")

// hash a password (custom cost, 4~31; 10 or 12 recommended)
hashed, err := hash.BcryptHash("myPassword123", 12)

// verify a password
ok := hash.BcryptMatch(hashed, "myPassword123")  // true
ok = hash.BcryptMatch(hashed, "wrongPassword")   // false

// verify a password (returns an error)
err := hash.BcryptCompare(hashed, "myPassword123") // nil = match

// check whether a string is a bcrypt hash
hash.BcryptIsHashed("$2a$10$abc...") // true
```

**Cost parameter reference**:

| Constant | Value | Description |
| ------ | ------ | ------ |
| `BcryptCostMin` | 4 | Minimum cost (fastest, least secure) |
| `BcryptCostDefault` | 10 | Default cost |
| `BcryptCostMax` | 31 | Maximum cost (slowest, most secure) |

### Secure comparison

Uses constant-time comparison to prevent timing attacks:

```go
// compare byte slices
hash.Equal([]byte("hash1"), []byte("hash2"))

// compare hex strings
hash.EqualHex("aaf4c61d...", "aaf4c61d...")
```

### Encoding helpers

```go
// encode
hexStr := hash.HexEncode([]byte("hello")) // "68656c6c6f"

// decode
data, err := hash.HexDecode("68656c6c6f") // []byte("hello")
```

## Complete example

```go
package main

import (
    "fmt"
    "os"

    "github.com/chihqiang/infra-go/hash"
    "github.com/chihqiang/infra-go/logger"
)

func main() {
    // --- basic hashing ---
    fmt.Println("=== Basic hashing ===")
    fmt.Println("MD5:    ", hash.MD5String("hello"))
    fmt.Println("SHA1:   ", hash.SHA1String("hello"))
    fmt.Println("SHA256: ", hash.SHA256String("hello"))
    fmt.Println("SHA512: ", hash.SHA512String("hello"))
    fmt.Println("SHA3:   ", hash.SHA3_256String("hello"))

    // --- HMAC ---
    fmt.Println("\n=== HMAC ===")
    fmt.Println("HMAC-SHA256:", hash.HMACSHA256String("secret", "hello world"))

    // --- Bcrypt password hashing ---
    fmt.Println("\n=== Bcrypt ===")
    password := "mySecretPassword"
    hashed, err := hash.BcryptHashDefault(password)
    if err != nil {
        logger.Fatal("failed to hash password", logger.Err(err))
    }
    fmt.Println("Hashed:", hashed)
    fmt.Println("Match: ", hash.BcryptMatch(hashed, password))

    // --- file hashing ---
    fmt.Println("\n=== File hashing ===")
    tmpFile := "/tmp/test.txt"
    os.WriteFile(tmpFile, []byte("hello world"), 0o644)

    fileMD5, err := hash.FileMD5(tmpFile)
    if err != nil {
        logger.Fatal("failed to compute file MD5", logger.Err(err))
    }
    fmt.Println("File MD5:", fileMD5)
    fmt.Println("Match:   ", hash.EqualHex(fileMD5, hash.MD5String("hello world")))
}
```

## Security recommendations

- **Password storage**: use `BcryptHashDefault`; never store passwords with MD5/SHA256
- **MD5/SHA1**: no longer secure; use them only for non-security purposes such as data checks and deduplication
- **HMAC**: use it for API signing, webhook signature verification, etc.
- **Bcrypt cost**: 12 or higher is recommended in production
- **Hash comparison**: always use `Equal` / `EqualHex`, never `==`
