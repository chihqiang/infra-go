package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// ossStorage is the Alibaba Cloud OSS storage implementation.
type ossStorage struct {
	client *oss.Client
	bucket *oss.Bucket
	url    string
}

// NewOSS creates an Alibaba Cloud OSS storage instance from the configuration.
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

// resolveOSSURL resolves the OSS file access domain.
// It prefers the URL from the configuration (CDN domain) and falls back to
// "https://{bucket}.{endpoint}" when that is empty.
// It also handles the case where endpoint already carries a protocol prefix.
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
		// Fall back to simple concatenation when parsing fails
		return "https://" + cfg.Bucket + "." + cfg.Endpoint
	}
	u.Host = cfg.Bucket + "." + u.Host
	u.Path = ""
	return u.String()
}

// Write writes content to the given OSS path.
// The OSS SDK does not support context cancellation, so the ctx state is checked
// before issuing the call to fail fast.
func (s *ossStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write OSS object %q: %w", path, err)
	}
	if err := s.bucket.PutObject(path, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("storage: failed to write OSS object %q: %w", path, err)
	}
	return nil
}

// Read reads the full content of the object at the given OSS path.
func (s *ossStorage) Read(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: read OSS object %q: %w", path, err)
	}
	body, err := s.bucket.GetObject(path)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read OSS object %q: %w", path, err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read OSS object %q body: %w", path, err)
	}
	return data, nil
}

// Exists reports whether the object at the given OSS path exists.
func (s *ossStorage) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("storage: check OSS object %q: %w", path, err)
	}
	found, err := s.bucket.IsObjectExist(path)
	if err != nil {
		return false, fmt.Errorf("storage: failed to check OSS object %q: %w", path, err)
	}
	return found, nil
}

// Delete removes the object at the given OSS path and returns the number of
// removed objects.
func (s *ossStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete OSS object %q: %w", path, err)
	}
	if err := s.bucket.DeleteObject(path); err != nil {
		return 0, fmt.Errorf("storage: failed to delete OSS object %q: %w", path, err)
	}
	return 1, nil
}

// URL builds the full OSS access URL from the given path.
func (s *ossStorage) URL(_ context.Context, path string) (string, error) {
	return buildURL(s.url, path)
}
