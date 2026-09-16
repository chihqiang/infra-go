package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// localStorage is the local filesystem storage implementation.
// It writes files to a local disk directory, which suits development or
// single-machine scenarios and can serve as a local stand-in for cloud storage.
type localStorage struct {
	root string // local root directory (absolute or relative path)
	url  string // access URL prefix (optional, e.g. http://localhost:8080/static); empty yields a file:// URL
}

// NewLocal creates a local filesystem storage instance from the configuration.
func NewLocal(cfg *LocalConfig) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage: local config is nil")
	}
	root := strings.TrimSpace(cfg.RootDir)
	if root == "" {
		return nil, fmt.Errorf("storage: local root_dir is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: invalid local root_dir %q: %w", root, err)
	}
	return &localStorage{
		root: abs,
		url:  strings.TrimRight(cfg.URL, "/"),
	}, nil
}

// errPathEscapesRoot indicates that the given path would escape the storage root.
var errPathEscapesRoot = errors.New("path escapes root directory")

// safePath converts a storage path (key) into an absolute local file path below root.
//
// Security constraint: path may come from untrusted input (an uploaded file name
// or a user supplied key), so any path escaping root must be rejected; otherwise
// filepath.Join would Clean away ".." and allow arbitrary file reads / writes /
// deletes (for example path="../../etc/passwd").
//
// Rejected forms: an empty path, an absolute path, and a path containing ".."
// that walks back out of root. A leading "/" is trimmed (consistent with the
// semantics of buildURL), so "/a/b.txt" is equivalent to "a/b.txt" and still
// stays below root.
//
// Note: this function only performs lexical validation and does not resolve
// symbolic links. A symlink inside the root directory that points outside root
// can still be reached indirectly; to guard against that, restrict write
// permissions on the root directory so untrusted parties cannot create symlinks
// inside it.
func (s *localStorage) safePath(path string) (string, error) {
	rel := filepath.FromSlash(strings.TrimLeft(filepath.ToSlash(path), "/"))
	if rel == "" {
		return "", fmt.Errorf("%w: path is empty", errPathEscapesRoot)
	}
	// IsLocal rejects absolute paths and ".." walks that escape the current directory.
	if !filepath.IsLocal(rel) {
		return "", errPathEscapesRoot
	}
	full := filepath.Join(s.root, rel)
	// Second line of defence: Join already cleans the path, but confirm here that
	// the result really lies below root.
	if full != s.root && !strings.HasPrefix(full, s.root+string(os.PathSeparator)) {
		return "", errPathEscapesRoot
	}
	return full, nil
}

// Write writes content to the given path on the local filesystem.
// Parent directories required by the path are created automatically; an existing
// file with the same name is overwritten.
func (s *localStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write local file %q: %w", path, err)
	}
	full, err := s.safePath(path)
	if err != nil {
		return fmt.Errorf("storage: write local file %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("storage: failed to create local directory for %q: %w", path, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("storage: failed to write local file %q: %w", path, err)
	}
	return nil
}

// Read reads the content of the file at the given path on the local filesystem.
// It returns an error when the file does not exist (the *PathError from the
// underlying os.ReadFile). It returns an error when path escapes the root
// directory (without touching the filesystem).
func (s *localStorage) Read(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: read local file %q: %w", path, err)
	}
	full, err := s.safePath(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read local file %q: %w", path, err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to read local file %q: %w", path, err)
	}
	return data, nil
}

// Exists reports whether the file at the given path exists on the local filesystem.
// It returns an error when path escapes the root directory (treated as not existing).
func (s *localStorage) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("storage: check local file %q: %w", path, err)
	}
	full, err := s.safePath(path)
	if err != nil {
		return false, fmt.Errorf("storage: check local file %q: %w", path, err)
	}
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: failed to check local file %q: %w", path, err)
	}
	return true, nil
}

// Delete removes the file at the given path on the local filesystem and returns
// the number of removed files. A missing file counts as already deleted
// (returns 0) and is not an error. It returns an error when path escapes the root
// directory (nothing is deleted).
func (s *localStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete local file %q: %w", path, err)
	}
	full, err := s.safePath(path)
	if err != nil {
		return 0, fmt.Errorf("storage: delete local file %q: %w", path, err)
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("storage: failed to delete local file %q: %w", path, err)
	}
	return 1, nil
}

// URL builds the access URL of a local file.
// When a URL prefix is configured the address is joined under that prefix
// (typically when a static file server is mounted); otherwise it returns a local
// absolute path with the file:// scheme.
// It returns an error when path escapes the root directory.
func (s *localStorage) URL(_ context.Context, path string) (string, error) {
	if s.url != "" {
		return buildURL(s.url, path)
	}
	full, err := s.safePath(path)
	if err != nil {
		return "", fmt.Errorf("storage: build local URL for %q: %w", path, err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(full)}).String(), nil
}
