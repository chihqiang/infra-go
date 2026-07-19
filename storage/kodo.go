package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/qiniu/go-sdk/v7/auth/qbox"
	qstorage "github.com/qiniu/go-sdk/v7/storage"
)

// kodoStorage 七牛云 KODO 存储实现。
type kodoStorage struct {
	mac           *qbox.Mac
	bucket        string
	storageConfig *qstorage.Config
	url           string
}

// NewKODO 根据配置创建七牛云 KODO 存储实例。
func NewKODO(cfg *KODOConfig) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage: KODO config is nil")
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("storage: KODO access key is required")
	}
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("storage: KODO secret key is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: KODO bucket is required")
	}

	region := cfg.Region
	if region == "" {
		region = "z0"
	}

	zone, ok := kodoRegions[region]
	if !ok {
		return nil, fmt.Errorf("storage: unsupported KODO region %q, supported: z0, z1, z2, na0, as0", region)
	}

	return &kodoStorage{
		mac:    qbox.NewMac(cfg.AccessKey, cfg.SecretKey),
		bucket: cfg.Bucket,
		storageConfig: &qstorage.Config{
			Region: zone,
		},
		url: cfg.URL,
	}, nil
}

// kodoRegions 七牛云存储区域映射。
var kodoRegions = map[string]*qstorage.Zone{
	"z0":  &qstorage.ZoneHuadong,  // 华东
	"z1":  &qstorage.ZoneHuabei,   // 华北
	"z2":  &qstorage.ZoneHuanan,   // 华南
	"na0": &qstorage.ZoneBeimei,   // 北美
	"as0": &qstorage.ZoneXinjiapo, // 东南亚
}

// uploadToken 生成上传凭证。
func (s *kodoStorage) uploadToken() string {
	putPolicy := qstorage.PutPolicy{
		Scope: s.bucket,
	}
	return putPolicy.UploadToken(s.mac)
}

// Write 将内容写入 KODO 指定路径。
func (s *kodoStorage) Write(path string, content []byte) error {
	formUploader := qstorage.NewFormUploader(s.storageConfig)
	dataLen := int64(len(content))
	err := formUploader.Put(context.Background(),
		&qstorage.PutRet{},
		s.uploadToken(),
		path,
		bytes.NewReader(content),
		dataLen,
		&qstorage.PutExtra{},
	)
	if err != nil {
		return fmt.Errorf("storage: failed to write KODO object %q: %w", path, err)
	}
	return nil
}

// Delete 删除 KODO 指定路径的对象，返回删除的对象数量。
func (s *kodoStorage) Delete(path string) (int64, error) {
	bucketManager := qstorage.NewBucketManager(s.mac, s.storageConfig)
	rets, err := bucketManager.Batch([]string{qstorage.URIDelete(s.bucket, path)})
	if err != nil {
		return 0, fmt.Errorf("storage: failed to delete KODO object %q: %w", path, err)
	}
	for _, ret := range rets {
		if ret.Code != http.StatusOK {
			return 0, fmt.Errorf("storage: failed to delete KODO object %q, code: %d, error: %s",
				path, ret.Code, ret.Data.Error)
		}
	}
	return 1, nil
}

// URL 根据路径生成完整的 KODO 访问 URL。
// 使用七牛云 SDK 的 MakePublicURL 生成标准的公开访问 URL。
// 若配置中未设置 URL 则返回错误。
func (s *kodoStorage) URL(path string) (string, error) {
	if s.url == "" {
		return "", fmt.Errorf("storage: KODO URL is empty, please set URL field in config")
	}
	return qstorage.MakePublicURL(s.url, path), nil
}
