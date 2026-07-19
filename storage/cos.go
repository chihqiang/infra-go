package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// cosStorage 腾讯云 COS 存储实现。
type cosStorage struct {
	client *cos.Client
	url    string
}

// NewCOS 根据配置创建腾讯云 COS 存储实例。
func NewCOS(cfg *COSConfig) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage: COS config is nil")
	}
	if cfg.BucketURL == "" {
		return nil, fmt.Errorf("storage: COS bucket URL is required")
	}
	if cfg.SecretID == "" {
		return nil, fmt.Errorf("storage: COS secret ID is required")
	}
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("storage: COS secret key is required")
	}

	bucketURL, err := url.Parse(cfg.BucketURL)
	if err != nil {
		return nil, fmt.Errorf("storage: invalid COS bucket URL %q: %w", cfg.BucketURL, err)
	}

	client := cos.NewClient(
		&cos.BaseURL{
			BucketURL: bucketURL,
		},
		&http.Client{
			Transport: &cos.AuthorizationTransport{
				SecretID:  cfg.SecretID,
				SecretKey: cfg.SecretKey,
			},
		},
	)

	return &cosStorage{
		client: client,
		url:    resolveCOSURL(cfg),
	}, nil
}

// resolveCOSURL 解析 COS 文件访问域名。
// 优先使用配置中的 URL（CDN 域名），为空时默认使用 BucketURL。
func resolveCOSURL(cfg *COSConfig) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	return cfg.BucketURL
}

// Write 将内容写入 COS 指定路径。
func (s *cosStorage) Write(path string, content []byte) error {
	_, err := s.client.Object.Put(context.Background(), path, strings.NewReader(string(content)), nil)
	if err != nil {
		return fmt.Errorf("storage: failed to write COS object %q: %w", path, err)
	}
	return nil
}

// Delete 删除 COS 指定路径的对象，返回删除的对象数量。
func (s *cosStorage) Delete(path string) (int64, error) {
	resp, err := s.client.Object.Delete(context.Background(), path)
	if err != nil {
		return 0, fmt.Errorf("storage: failed to delete COS object %q: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("storage: failed to delete COS object %q, status code: %d", path, resp.StatusCode)
	}
	return 1, nil
}

// URL 根据路径拼接完整的 COS 访问 URL。
func (s *cosStorage) URL(path string) (string, error) {
	return buildURL(s.url, path)
}
