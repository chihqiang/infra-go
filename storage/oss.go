package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// ossStorage 阿里云 OSS 存储实现。
type ossStorage struct {
	client *oss.Client
	bucket *oss.Bucket
	url    string
}

// NewOSS 根据配置创建阿里云 OSS 存储实例。
func NewOSS(cfg *OSSConfig) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage: OSS config is nil")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage: OSS endpoint is required")
	}
	if cfg.AccessKeyID == "" {
		return nil, fmt.Errorf("storage: OSS access key ID is required")
	}
	if cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("storage: OSS access key secret is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: OSS bucket is required")
	}

	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create OSS client: %w", err)
	}

	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to get OSS bucket %q: %w", cfg.Bucket, err)
	}

	return &ossStorage{
		client: client,
		bucket: bucket,
		url:    resolveOSSURL(cfg),
	}, nil
}

// resolveOSSURL 解析 OSS 文件访问域名。
// 优先使用配置中的 URL（CDN 域名），为空时默认使用 "https://{bucket}.{endpoint}"。
// 兼容 endpoint 已包含协议前缀的情况。
func resolveOSSURL(cfg *OSSConfig) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	ep := cfg.Endpoint
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil {
		// 解析失败时回退到简单拼接
		return "https://" + cfg.Bucket + "." + cfg.Endpoint
	}
	u.Host = cfg.Bucket + "." + u.Host
	u.Path = ""
	return u.String()
}

// Write 将内容写入 OSS 指定路径。
// OSS SDK 不支持 context 取消，这里在发起调用前检查 ctx 状态实现快速失败。
func (s *ossStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write OSS object %q: %w", path, err)
	}
	if err := s.bucket.PutObject(path, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("storage: failed to write OSS object %q: %w", path, err)
	}
	return nil
}

// Delete 删除 OSS 指定路径的对象，返回删除的对象数量。
func (s *ossStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete OSS object %q: %w", path, err)
	}
	if err := s.bucket.DeleteObject(path); err != nil {
		return 0, fmt.Errorf("storage: failed to delete OSS object %q: %w", path, err)
	}
	return 1, nil
}

// URL 根据路径拼接完整的 OSS 访问 URL。
func (s *ossStorage) URL(_ context.Context, path string) (string, error) {
	return buildURL(s.url, path)
}
