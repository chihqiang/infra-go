package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Storage is the storage service interface, defining the basic operations of
// object storage. Four implementations are supported: local filesystem,
// Alibaba Cloud OSS, Tencent Cloud COS and Qiniu Cloud KODO.
//
// Note: the Alibaba Cloud OSS SDK does not support context cancellation. In the
// OSS implementation the context passed to Write/Delete/Read/Exists is only used
// for fast-fail detection (the ctx state is checked before issuing the SDK call);
// it does not interrupt an SDK call that has already been issued.
type Storage interface {
	// Write writes content to the given path.
	// ctx controls request timeout and cancellation.
	// path is the object path (key) inside the bucket, content is the file content.
	Write(ctx context.Context, path string, content []byte) error

	// Read reads the full content of the object at the given path.
	// ctx controls request timeout and cancellation.
	// path is the object path (key) inside the bucket.
	// It returns an error when the object does not exist (each driver returns the
	// error of its own SDK; use Exists first to check for existence).
	Read(ctx context.Context, path string) ([]byte, error)

	// Exists reports whether the object at the given path exists.
	// ctx controls request timeout and cancellation.
	// path is the object path (key) inside the bucket.
	Exists(ctx context.Context, path string) (bool, error)

	// Delete removes the object at the given path and returns the number of
	// removed objects.
	// ctx controls request timeout and cancellation.
	// path is the object path (key) inside the bucket.
	Delete(ctx context.Context, path string) (int64, error)

	// URL builds the full access URL from the given path.
	// ctx is reserved for future extension (all current implementations just
	// concatenate URL parts locally and do not depend on context).
	// path is the object path (key) inside the bucket.
	URL(ctx context.Context, path string) (string, error)
}

// buildURL joins the base domain and the path into a full URL.
// It handles a trailing slash on base and a leading slash on path, and
// URL-encodes path.
func buildURL(base, path string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("storage: base URL is empty, please set URL field in config")
	}
	base = strings.TrimRight(base, "/")
	path = strings.TrimLeft(path, "/")
	// URL-encode special characters in path (the / separator is kept)
	encoded := (&url.URL{Path: path}).String()
	return base + "/" + encoded, nil
}

// Driver is the storage driver type.
type Driver string

const (
	// DriverOSS is the Alibaba Cloud OSS storage.
	DriverOSS Driver = "oss"
	// DriverCOS is the Tencent Cloud COS storage.
	DriverCOS Driver = "cos"
	// DriverKODO is the Qiniu Cloud KODO storage.
	DriverKODO Driver = "kodo"
	// DriverLocal is the local filesystem storage.
	DriverLocal Driver = "local"
)

// Storages is a collection of storage instances: the key is the instance alias
// (such as "images" or "docs") and the value is an instantiated Storage
// implementation. Separate credentials or drivers can coexist under aliases.
type Storages map[string]Storage

// Get returns the storage instance registered under the given alias.
func (s Storages) Get(name string) (Storage, bool) {
	st, ok := s[name]
	return st, ok
}

// Write writes content through the storage instance registered under the given alias.
// name is the storage instance alias, path is the object path (key) in that storage.
func (s Storages) Write(ctx context.Context, name, path string, content []byte) error {
	st, ok := s[name]
	if !ok {
		return fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Write(ctx, path, content)
}

// Read reads the full content of an object from the storage instance registered
// under the given alias.
func (s Storages) Read(ctx context.Context, name, path string) ([]byte, error) {
	st, ok := s[name]
	if !ok {
		return nil, fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Read(ctx, path)
}

// Exists reports whether an object exists in the storage instance registered
// under the given alias.
func (s Storages) Exists(ctx context.Context, name, path string) (bool, error) {
	st, ok := s[name]
	if !ok {
		return false, fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Exists(ctx, path)
}

// Delete removes an object from the storage instance registered under the given
// alias and returns the number of removed objects.
func (s Storages) Delete(ctx context.Context, name, path string) (int64, error) {
	st, ok := s[name]
	if !ok {
		return 0, fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Delete(ctx, path)
}

// URL builds the full access URL for the storage instance registered under the
// given alias.
func (s Storages) URL(ctx context.Context, name, path string) (string, error) {
	st, ok := s[name]
	if !ok {
		return "", fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.URL(ctx, path)
}
