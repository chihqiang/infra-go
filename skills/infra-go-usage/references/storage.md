# storage

A unified object storage interface supporting the local filesystem, Alibaba Cloud OSS, Tencent
Cloud COS, and Qiniu KODO, with a factory that selects the implementation from the config.

## Architecture

```bash
Config.Driver ──▶ New() ──┬── "local" ──▶ NewLocal() ──▶ localStorage
                          ├── "oss"   ──▶ NewOSS()   ──▶ ossStorage
                          ├── "cos"   ──▶ NewCOS()   ──▶ cosStorage
                          └── "kodo"  ──▶ NewKODO()  ──▶ kodoStorage
```

All implementations satisfy the `Storage` interface (5 methods):

```go
type Storage interface {
    Write(ctx context.Context, path string, content []byte) error      // write an object
    Read(ctx context.Context, path string) ([]byte, error)             // read the full object content
    Exists(ctx context.Context, path string) (bool, error)             // check whether an object exists
    Delete(ctx context.Context, path string) (int64, error)            // delete objects, returning the count
    URL(ctx context.Context, path string) (string, error)              // build the object access URL
}
```

ctx controls request timeout and cancellation. Note that the cloud SDKs differ in their context
support: the implementations uniformly **check `ctx.Err()` before starting an SDK call for a
quick failure** and never interrupt an SDK call already in flight — the Alibaba Cloud OSS SDK
does not support native context cancellation; KODO's Delete uses the Batch API, which has no
context support, and its Exists relies on a server-side Stat query; KODO's Read performs an HTTP
GET against the public domain and requires `URL` to be configured (private buckets cannot be
downloaded yet).

## Quick start

```go
package main

import (
    "context"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/storage"
)

func main() {
    // --- create through the factory (recommended) ---
    s, err := storage.New(storage.Config{
        Driver: storage.DriverOSS,
        OSS: &storage.OSSConfig{
            Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
            AccessKeyID:     "your-access-key-id",
            AccessKeySecret: "your-access-key-secret",
            Bucket:          "your-bucket",
        },
    })
    if err != nil {
        logger.Fatal("failed to create storage", logger.Err(err))
    }

    // write a file
    ctx := context.Background()
    err = s.Write(ctx, "test/hello.txt", []byte("hello world"))
    if err != nil {
        logger.Fatal("failed to write file", logger.Err(err))
    }

    // get the file access URL
    u, err := s.URL(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to get file URL", logger.Err(err))
    }
    logger.Infof("file URL: %s", u)

    // check whether the object exists
    exists, err := s.Exists(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to check file", logger.Err(err))
    }
    logger.Infof("file exists: %v", exists)

    // read the object content (read back for verification/processing, etc.)
    data, err := s.Read(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to read file", logger.Err(err))
    }
    logger.Infof("file content: %s", data)

    // delete the file
    count, err := s.Delete(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to delete file", logger.Err(err))
    }
    logger.Infof("deleted %d object(s)", count)
}
```

## Reading and existence checks (Read / Exists)

`Read` reads the full object content and returns it as `[]byte`, suited to reading back modest
payloads for verification, image processing, template rendering, and similar; it returns an error
when the object does not exist.
`Exists` checks whether an object exists using each driver's metadata query (Head/Stat), suited to
de-duplication before upload, resource availability checks, and similar.

```go
ctx := context.Background()

// check first, then read (a missing object does not trigger a Read error)
if ok, err := s.Exists(ctx, "img/a.png"); err != nil {
    logger.Fatal("check failed", logger.Err(err))
} else if !ok {
    logger.Infof("object not exists")
} else {
    data, err := s.Read(ctx, "img/a.png")
    if err != nil {
        logger.Fatal("read failed", logger.Err(err))
    }
    logger.Infof("size=%d", len(data))
}
```

Implementation notes per driver:

| Driver | Read | Exists |
| ------ | ------ | ------ |
| `local` | `os.ReadFile` to read a local file | `os.Stat`; returns `(false, nil)` when the file does not exist |
| `oss` | `bucket.GetObject` → `io.ReadAll` | `bucket.IsObjectExist` |
| `cos` | `Object.Get` → `resp.Body` | `Object.IsExist` |
| `kodo` | requires the public access domain `URL`, otherwise it errors; private buckets unsupported | `Stat`; HTTP 612 (no such file) counts as non-existent |

## Configuration

### Common config

```go
type Config struct {
    Driver Driver        // storage driver, one of "local", "oss", "cos", "kodo"; required
    Local  *LocalConfig  // local filesystem config, used when Driver is "local"
    OSS    *OSSConfig    // Alibaba Cloud OSS config, used when Driver is "oss"
    COS    *COSConfig    // Tencent Cloud COS config, used when Driver is "cos"
    KODO   *KODOConfig   // Qiniu KODO config, used when Driver is "kodo"
}
```

### Local filesystem

Writes files straight into a local disk directory; suited to development/single-node setups or as
a local replacement for cloud storage, and needs no cloud credentials at all:

```go
type LocalConfig struct {
    RootDir string // storage root directory; required. Files land under it; path is relative to it
    URL     string // access URL prefix (optional), e.g. http://localhost:8080/static; empty means a file:// path
}
```

Example usage:

```go
// create through the factory
s, err := storage.New(storage.Config{
    Driver: storage.DriverLocal,
    Local: &storage.LocalConfig{
        RootDir: "./data/storage",
        URL:     "http://localhost:8080/static", // optional
    },
})

// or create directly
s, err := storage.NewLocal(&storage.LocalConfig{
    RootDir: "./data/storage",
})

ctx := context.Background()
if err := s.Write(ctx, "a/b.txt", []byte("hello")); err != nil { /* directories are created automatically */ }
u, err := s.URL(ctx, "a/b.txt") // "file:///.../data/storage/a/b.txt" or the configured prefix
n, err := s.Delete(ctx, "a/b.txt")
```

> **Path safety**: a `path` passed in is always treated as relative to `RootDir`, and any path that
escapes the root is rejected with an error (`path escapes root directory`), including `..`
traversal, paths that still escape after `..` normalization, and empty paths. A leading `/` is
trimmed, so `"/a/b.txt"` and `"a/b.txt"` are equivalent.
>
> Since `path` may come from untrusted input (upload file names, user-supplied keys), do not build
> absolute paths upstream just to bypass this check. The protection is lexical and does not resolve
> symlinks — to guard against symlink escapes, restrict write permissions on `RootDir`.

### Alibaba Cloud OSS

```go
type OSSConfig struct {
    Endpoint        string // endpoint, e.g. "oss-cn-hangzhou.aliyuncs.com"
    AccessKeyID     string // AccessKey ID
    AccessKeySecret string // AccessKey Secret
    Bucket          string // bucket name
    URL             string // file access domain (CDN); defaults to https://{bucket}.{endpoint} when empty
}
```

Example usage:

```go
// create through the factory
s, err := storage.New(storage.Config{
    Driver: storage.DriverOSS,
    OSS: &storage.OSSConfig{
        Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
        AccessKeyID:     "your-access-key-id",
        AccessKeySecret: "your-access-key-secret",
        Bucket:          "your-bucket",
        URL:             "https://cdn.example.com", // optional; defaults to https://{bucket}.{endpoint}
    },
})

// or create directly
s, err := storage.NewOSS(&storage.OSSConfig{
    Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
    AccessKeyID:     "your-access-key-id",
    AccessKeySecret: "your-access-key-secret",
    Bucket:          "your-bucket",
    URL:             "https://cdn.example.com", // optional
})
```

Full region list: <https://help.aliyun.com/zh/oss/user-guide/regions-and-endpoints>

### Tencent Cloud COS

```go
type COSConfig struct {
    BucketURL string // bucket URL, e.g. "https://bucket-name.cos.ap-beijing.myqcloud.com"
    SecretID  string // SecretID
    SecretKey string // SecretKey
    URL       string // file access domain (CDN); falls back to BucketURL when empty
}
```

Example usage:

```go
// create through the factory
s, err := storage.New(storage.Config{
    Driver: storage.DriverCOS,
    COS: &storage.COSConfig{
        BucketURL: "https://bucket-name.cos.ap-beijing.myqcloud.com",
        SecretID:  "your-secret-id",
        SecretKey: "your-secret-key",
        URL:       "https://cdn.example.com", // optional; falls back to BucketURL when empty
    },
})

// or create directly
s, err := storage.NewCOS(&storage.COSConfig{
    BucketURL: "https://bucket-name.cos.ap-beijing.myqcloud.com",
    SecretID:  "your-secret-id",
    SecretKey: "your-secret-key",
    URL:       "https://cdn.example.com", // optional
})
```

Bucket list: <https://console.cloud.tencent.com/cos5/bucket>

### Qiniu KODO

```go
type KODOConfig struct {
    AccessKey string // AccessKey
    SecretKey string // SecretKey
    Bucket    string // bucket name
    Region    string // storage region, default "z0"
    URL       string // file access domain (CDN); Qiniu requires a bound domain, mandatory for URL()
}
```

Supported regions:

| Region value | Description |
| -------- | ------ |
| `z0` | East China (default) |
| `z1` | North China |
| `z2` | South China |
| `na0` | North America |
| `as0` | Southeast Asia |

Example usage:

```go
// create through the factory
s, err := storage.New(storage.Config{
    Driver: storage.DriverKODO,
    KODO: &storage.KODOConfig{
        AccessKey: "your-access-key",
        SecretKey: "your-secret-key",
        Bucket:    "your-bucket",
        Region:    "z0", // optional, defaults to z0
        URL:       "https://cdn.example.com", // required when calling URL()
    },
})

// or create directly
s, err := storage.NewKODO(&storage.KODOConfig{
    AccessKey: "your-access-key",
    SecretKey: "your-secret-key",
    Bucket:    "your-bucket",
    URL:       "https://cdn.example.com",
})
```

Region list: <https://developer.qiniu.com/kodo/manual/1671/region-endpoint-fq>

## MustNew

Panics on error; suited to initialization:

```go
// Storage is a stateless client (the 5 interface methods Write/Read/Exists/Delete/URL;
// no Close, nothing to release)
s := storage.MustNew(storage.Config{
    Driver: storage.DriverOSS,
    OSS:    &storage.OSSConfig{...},
})
```

## Multiple storage instances: Storages

`Storages` is a `map[string]Storage` keyed by instance alias, holding instantiated `Storage`
values. It lets one collection manage several buckets / credential sets (say separate avatar,
attachment, and video buckets), where each instance may have its own access URL. Instantiate them
separately first, then build the `Storages` collection with a type conversion:

```go
images, err := storage.NewOSS(&storage.OSSConfig{
    Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
    AccessKeyID:     "your-access-key-id",
    AccessKeySecret: "your-access-key-secret",
    Bucket:          "prod-images",
    URL:             "https://img.example.com",
})
docs, err := storage.NewOSS(&storage.OSSConfig{
    Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
    AccessKeyID:     "your-access-key-id",
    AccessKeySecret: "your-access-key-secret",
    Bucket:          "prod-docs",
    URL:             "https://docs.example.com",
})

storages := storage.Storages(map[string]Storage{
    "images": images,
    "docs":   docs,
})

ctx := context.Background()

// write / read / check existence / delete / get URL by alias
err = storages.Write(ctx, "images", "a.png", []byte("..."))
data, err := storages.Read(ctx, "images", "a.png")
ok, err := storages.Exists(ctx, "images", "a.png")
count, err := storages.Delete(ctx, "images", "a.png")
u, err := storages.URL(ctx, "docs", "manual.pdf")

// or fetch a single instance
s, ok := storages.Get("images")
```

`Storages` also supports several drivers coexisting (OSS + COS + KODO mixed).
