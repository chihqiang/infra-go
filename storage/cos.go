package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// cosStorage is the Tencent Cloud COS storage implementation.
type cosStorage struct {
	client *cos.Client
	url    string
}

// NewCOS creates a Tencent Cloud COS storage instance from the configuration.
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

// resolveCOSURL resolves the COS file access domain.
// It prefers the URL from the configuration (CDN domain) and falls back to
// BucketURL when that is empty.
func resolveCOSURL(cfg *COSConfig) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	return cfg.BucketURL
}

// Write writes content to the given COS path.
func (s *cosStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write COS object %q: %w", path, err)
	}
	_, err := s.client.Object.Put(ctx, path, bytes.NewReader(content), nil)
	if err != nil {
		return fmt.Errorf("storage: failed to write COS object %q: %w", path, err)
	}
	return nil
}

// Read reads the full content of the object at the given COS path.
func (s *cosStorage) Read(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: read COS object %q: %w", path, err)
	}
	resp, err := s.client.Object.Get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read COS object %q: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read COS object %q body: %w", path, err)
	}
	return data, nil
}

// Exists reports whether the object at the given COS path exists.
func (s *cosStorage) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("storage: check COS object %q: %w", path, err)
	}
	found, err := s.client.Object.IsExist(ctx, path)
	if err != nil {
		return false, fmt.Errorf("storage: failed to check COS object %q: %w", path, err)
	}
	return found, nil
}

// Delete removes the object at the given COS path and returns the number of
// removed objects.
func (s *cosStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete COS object %q: %w", path, err)
	}
	resp, err := s.client.Object.Delete(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("storage: failed to delete COS object %q: %w", path, err)
	}
	// A successful COS delete returns 204 No Content (the SDK already treats
	// >=300 as an error), so the semantics here must be 2xx rather than 200 only;
	// otherwise a successful delete would be reported as a failure.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("storage: failed to delete COS object %q, status code: %d", path, resp.StatusCode)
	}
	return 1, nil
}

// URL builds the full COS access URL from the given path.
func (s *cosStorage) URL(_ context.Context, path string) (string, error) {
	return buildURL(s.url, path)
}
