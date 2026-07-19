# storage

统一对象存储接口，支持阿里云 OSS、腾讯云 COS 和七牛云 KODO，通过工厂模式根据配置自动选择实现。

## 架构

```bash
Config.Driver ──▶ New() ──┬── "oss"  ──▶ NewOSS()  ──▶ ossStorage
                          ├── "cos"  ──▶ NewCOS()  ──▶ cosStorage
                          └── "kodo" ──▶ NewKODO() ──▶ kodoStorage

所有实现都满足 Storage 接口：
    Write(path string, content []byte) error
    Delete(path string) (int64, error)
    URL(path string) (string, error)
```

## 快速开始

```go
package main

import (
    "log"

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
        log.Fatal(err)
    }

    // 写入文件
    err = s.Write("test/hello.txt", []byte("hello world"))
    if err != nil {
        log.Fatal(err)
    }

    // 获取文件访问 URL
    u, err := s.URL("test/hello.txt")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("file URL: %s", u)

    // 删除文件
    count, err := s.Delete("test/hello.txt")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("deleted %d object(s)", count)
}
```

## 配置

### 通用配置

```go
type Config struct {
    Driver Driver       // 存储驱动类型，支持 "oss"、"cos"、"kodo"，必填
    OSS    *OSSConfig   // 阿里云 OSS 配置，Driver 为 "oss" 时使用
    COS    *COSConfig   // 腾讯云 COS 配置，Driver 为 "cos" 时使用
    KODO   *KODOConfig  // 七牛云 KODO 配置，Driver 为 "kodo" 时使用
}
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
defer log.FatalIfFunc(s.Close, "close storage failed")
```

## 文件说明

| 文件 | 说明 |
| ------ | ------ |
| `storage.go` | `Storage` 接口定义与驱动类型常量 |
| `config.go` | `Config`、`OSSConfig`、`COSConfig`、`KODOConfig` 配置结构 |
| `oss.go` | 阿里云 OSS 存储实现 |
| `cos.go` | 腾讯云 COS 存储实现 |
| `kodo.go` | 七牛云 KODO 存储实现 |
| `factory.go` | 工厂方法，根据配置选择存储实现 |
