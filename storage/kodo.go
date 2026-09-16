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

// kodoStorage is the Qiniu Cloud KODO storage implementation.
type kodoStorage struct {
	mac           *qbox.Mac
	bucket        string
	storageConfig *qstorage.Config
	url           string
}

// NewKODO creates a Qiniu Cloud KODO storage instance from the configuration.
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

// kodoRegions maps Qiniu Cloud storage region names to zones.
var kodoRegions = map[string]*qstorage.Zone{
	"z0":  &qstorage.ZoneHuadong,  // East China
	"z1":  &qstorage.ZoneHuabei,   // North China
	"z2":  &qstorage.ZoneHuanan,   // South China
	"na0": &qstorage.ZoneBeimei,   // North America
	"as0": &qstorage.ZoneXinjiapo, // Southeast Asia
}

// uploadToken generates an upload credential.
func (s *kodoStorage) uploadToken() string {
	putPolicy := qstorage.PutPolicy{
		Scope: s.bucket,
	}
	return putPolicy.UploadToken(s.mac)
}

// Write writes content to the given KODO path.
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

// Read downloads the full content of the object at the given KODO path.
// It requires a public access domain in URL (the same one used by URL());
// downloading from a private bucket needs a signed URL and is not supported yet.
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
	// Only 2xx is accepted: a CDN or reverse proxy may return other success
	// status codes such as 206, and accepting 200 only would misreport a
	// successful read as a failure.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("storage: failed to read KODO object %q, status code: %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read KODO object %q body: %w", path, err)
	}
	return data, nil
}

// Exists reports whether the object at the given KODO path exists.
// It fetches the object metadata via Stat: HTTP 612 (no such file) means the
// object does not exist.
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

// Delete removes the object at the given KODO path and returns the number of
// removed objects.
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

// URL builds the full KODO access URL from the given path.
// It uses MakePublicURL from the Qiniu Cloud SDK to build the standard public
// access URL. It returns an error when URL is not set in the configuration.
func (s *kodoStorage) URL(_ context.Context, path string) (string, error) {
	if s.url == "" {
		return "", fmt.Errorf("storage: KODO URL is empty, please set URL field in config")
	}
	return qstorage.MakePublicURL(s.url, path), nil
}
