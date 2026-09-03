# storage

统一对象存储接口，支持本地文件系统、阿里云 OSS、腾讯云 COS 和七牛云 KODO，通过工厂模式根据配置自动选择实现。

## 架构

```bash
Config.Driver ──▶ New() ──┬── "local" ──▶ NewLocal() ──▶ localStorage
                          ├── "oss"   ──▶ NewOSS()   ──▶ ossStorage
                          ├── "cos"   ──▶ NewCOS()   ──▶ cosStorage
                          └── "kodo"  ──▶ NewKODO()  ──▶ kodoStorage

所有实现都满足 Storage 接口：
    Write(ctx context.Context, path string, content []byte) error
    Delete(ctx context.Context, path string) (int64, error)
    URL(ctx context.Context, path string) (string, error)
```

ctx 用于控制请求超时和取消。注意：阿里云 OSS SDK 不支持原生 context 取消，
OSS 实现仅做快速失败检测；KODO 的 Delete 使用了不支持 context 的 Batch API，同样仅做快速失败检测。

## 快速开始

```go
package main

import (
    "context"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/storage"
)

func main() {
    // --- 通过工厂创建（推荐） ---
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

    // 写入文件
    ctx := context.Background()
    err = s.Write(ctx, "test/hello.txt", []byte("hello world"))
    if err != nil {
        logger.Fatal("failed to write file", logger.Err(err))
    }

    // 获取文件访问 URL
    u, err := s.URL(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to get file URL", logger.Err(err))
    }
    logger.Infof("file URL: %s", u)

    // 删除文件
    count, err := s.Delete(ctx, "test/hello.txt")
    if err != nil {
        logger.Fatal("failed to delete file", logger.Err(err))
    }
    logger.Infof("deleted %d object(s)", count)
}
```

## 配置

### 通用配置

```go
type Config struct {
    Driver Driver        // 存储驱动类型，支持 "local"、"oss"、"cos"、"kodo"，必填
    Local  *LocalConfig  // 本地文件系统配置，Driver 为 "local" 时使用
    OSS    *OSSConfig    // 阿里云 OSS 配置，Driver 为 "oss" 时使用
    COS    *COSConfig    // 腾讯云 COS 配置，Driver 为 "cos" 时使用
    KODO   *KODOConfig   // 七牛云 KODO 配置，Driver 为 "kodo" 时使用
}
```

### 本地文件系统 Local

将文件直接写入本地磁盘目录，适合开发/单机场景或作为云存储的本地替代，无需任何云凭证：

```go
type LocalConfig struct {
    RootDir string // 本地存储根目录，必填；文件写入此目录下，path 对应根目录下的相对路径
    URL     string // 访问 URL 前缀（可选），如 http://localhost:8080/static；为空时 URL() 返回 file:// 本地路径
}
```

使用示例：

```go
// 通过工厂创建
s, err := storage.New(storage.Config{
    Driver: storage.DriverLocal,
    Local: &storage.LocalConfig{
        RootDir: "./data/storage",
        URL:     "http://localhost:8080/static", // 可选
    },
})

// 或直接创建
s, err := storage.NewLocal(&storage.LocalConfig{
    RootDir: "./data/storage",
})

ctx := context.Background()
if err := s.Write(ctx, "a/b.txt", []byte("hello")); err != nil { /* 自动创建目录 */ }
u, err := s.URL(ctx, "a/b.txt") // "file:///.../data/storage/a/b.txt" 或配置前缀
n, err := s.Delete(ctx, "a/b.txt")
```

### 阿里云 OSS

```go
type OSSConfig struct {
    Endpoint        string // 访问域名，例如 "oss-cn-hangzhou.aliyuncs.com"
    AccessKeyID     string // AccessKey ID
    AccessKeySecret string // AccessKey Secret
    Bucket          string // 存储空间名称
    URL             string // 文件访问域名（CDN），为空时默认 https://{bucket}.{endpoint}
}
```

使用示例：

```go
// 通过工厂创建
s, err := storage.New(storage.Config{
    Driver: storage.DriverOSS,
    OSS: &storage.OSSConfig{
        Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
        AccessKeyID:     "your-access-key-id",
        AccessKeySecret: "your-access-key-secret",
        Bucket:          "your-bucket",
        URL:             "https://cdn.example.com", // 可选，为空时默认 https://{bucket}.{endpoint}
    },
})

// 或直接创建
s, err := storage.NewOSS(&storage.OSSConfig{
    Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
    AccessKeyID:     "your-access-key-id",
    AccessKeySecret: "your-access-key-secret",
    Bucket:          "your-bucket",
    URL:             "https://cdn.example.com", // 可选
})
```

完整区域列表参考：<https://help.aliyun.com/zh/oss/user-guide/regions-and-endpoints>

### 腾讯云 COS

```go
type COSConfig struct {
    BucketURL string // 存储桶地址，例如 "https://bucket-name.cos.ap-beijing.myqcloud.com"
    SecretID  string // SecretID
    SecretKey string // SecretKey
    URL       string // 文件访问域名（CDN），为空时默认使用 BucketURL
}
```

使用示例：

```go
// 通过工厂创建
s, err := storage.New(storage.Config{
    Driver: storage.DriverCOS,
    COS: &storage.COSConfig{
        BucketURL: "https://bucket-name.cos.ap-beijing.myqcloud.com",
        SecretID:  "your-secret-id",
        SecretKey: "your-secret-key",
        URL:       "https://cdn.example.com", // 可选，为空时默认使用 BucketURL
    },
})

// 或直接创建
s, err := storage.NewCOS(&storage.COSConfig{
    BucketURL: "https://bucket-name.cos.ap-beijing.myqcloud.com",
    SecretID:  "your-secret-id",
    SecretKey: "your-secret-key",
    URL:       "https://cdn.example.com", // 可选
})
```

存储桶列表参考：<https://console.cloud.tencent.com/cos5/bucket>

### 七牛云 KODO

```go
type KODOConfig struct {
    AccessKey string // AccessKey
    SecretKey string // SecretKey
    Bucket    string // 存储空间名称
    Region    string // 存储区域，默认 "z0"
    URL       string // 文件访问域名（CDN），七牛云必须绑定域名，调用 URL() 时必填
}
```

支持的区域：

| 区域值 | 说明 |
| -------- | ------ |
| `z0` | 华东（默认） |
| `z1` | 华北 |
| `z2` | 华南 |
| `na0` | 北美 |
| `as0` | 东南亚 |

使用示例：

```go
// 通过工厂创建
s, err := storage.New(storage.Config{
    Driver: storage.DriverKODO,
    KODO: &storage.KODOConfig{
        AccessKey: "your-access-key",
        SecretKey: "your-secret-key",
        Bucket:    "your-bucket",
        Region:    "z0", // 可选，默认 z0
        URL:       "https://cdn.example.com", // 调用 URL() 时必填
    },
})

// 或直接创建
s, err := storage.NewKODO(&storage.KODOConfig{
    AccessKey: "your-access-key",
    SecretKey: "your-secret-key",
    Bucket:    "your-bucket",
    URL:       "https://cdn.example.com",
})
```

区域列表参考：<https://developer.qiniu.com/kodo/manual/1671/region-endpoint-fq>

## MustNew

出错时 panic，适合初始化场景：

```go
s := storage.MustNew(storage.Config{
    Driver: storage.DriverOSS,
    OSS:    &storage.OSSConfig{...},
})
defer func() {
    if err := s.Close(); err != nil {
        logger.Fatal("close storage failed", logger.Err(err))
    }
}()
```

## 多存储实例 Storages

`Storages` 是 `map[string]Storage` 类型，key 为实例别名，value 为已实例化的 `Storage`，
支持一个集合管理多个存储桶/多套凭证（如不同业务的头像、附件、视频桶），每个实例可有
独立的访问地址。先分别实例化，再注入工厂：

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

storages := storage.NewStorages(map[string]Storage{
    "images": images,
    "docs":   docs,
})

ctx := context.Background()

// 按别名写入/删除/取 URL
err = storages.Write(ctx, "images", "a.png", []byte("..."))
count, err := storages.Delete(ctx, "images", "a.png")
u, err := storages.URL(ctx, "docs", "manual.pdf")

// 或取出单个实例
s, ok := storages.Get("images")
```

`Storages` 也支持多种驱动共存（OSS + COS + KODO 混用）。

## 文件说明

| 文件 | 说明 |
| ------ | ------ |
| `storage.go` | `Storage` 接口定义、驱动类型常量与 `Storages` 多实例集合 |
| `config.go` | `Config`、`LocalConfig`、`OSSConfig`、`COSConfig`、`KODOConfig` 配置结构 |
| `local.go` | 本地文件系统存储实现 |
| `oss.go` | 阿里云 OSS 存储实现 |
| `cos.go` | 腾讯云 COS 存储实现 |
| `kodo.go` | 七牛云 KODO 存储实现 |
| `new.go` | 工厂方法 `New`/`MustNew`，根据配置选择存储实现；`NewStorages` 注入实例组织多存储集合 |
