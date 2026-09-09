package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/qiniu/go-sdk/v7/auth/qbox"
	"github.com/qiniu/go-sdk/v7/client"
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
func (s *kodoStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write KODO object %q: %w", path, err)
	}
	formUploader := qstorage.NewFormUploader(s.storageConfig)
	dataLen := int64(len(content))
	err := formUploader.Put(ctx,
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

// Read 下载 KODO 指定路径对象的完整内容。
// 需要配置公开访问域名 URL（与 URL() 一致）；私有空间的下载需另行走签名 URL，当前不支持。
func (s *kodoStorage) Read(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: read KODO object %q: %w", path, err)
	}
	if s.url == "" {
		return nil, fmt.Errorf("storage: KODO URL is empty, please set URL field in config")
	}
	u := qstorage.MakePublicURL(s.url, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to build KODO read request for %q: %w", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read KODO object %q: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage: failed to read KODO object %q, status code: %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read KODO object %q body: %w", path, err)
	}
	return data, nil
}

// Exists 判断 KODO 指定路径的对象是否存在。
// 通过 Stat 获取对象元信息：HTTP 612（no such file）视为不存在。
func (s *kodoStorage) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("storage: check KODO object %q: %w", path, err)
	}
	bucketManager := qstorage.NewBucketManager(s.mac, s.storageConfig)
	_, err := bucketManager.Stat(s.bucket, path)
	if err == nil {
		return true, nil
	}
	var qerr *client.ErrorInfo
	if errors.As(err, &qerr) && qerr.Code == 612 {
		return false, nil
	}
	return false, fmt.Errorf("storage: failed to check KODO object %q: %w", path, err)
}

// Delete 删除 KODO 指定路径的对象，返回删除的对象数量。
func (s *kodoStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete KODO object %q: %w", path, err)
	}
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
func (s *kodoStorage) URL(_ context.Context, path string) (string, error) {
	if s.url == "" {
		return "", fmt.Errorf("storage: KODO URL is empty, please set URL field in config")
	}
	return qstorage.MakePublicURL(s.url, path), nil
}
