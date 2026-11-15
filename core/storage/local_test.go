package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewLocal ---

func TestNewLocal_NilConfig(t *testing.T) {
	_, err := NewLocal(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local config is nil")
}

func TestNewLocal_MissingRootDir(t *testing.T) {
	_, err := NewLocal(&LocalConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root_dir is required")
}

func TestNewLocal_Ok(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)
	ls, ok := s.(*localStorage)
	require.True(t, ok)
	assert.NotEmpty(t, ls.root)
}

// --- Write / URL / Delete ---

func TestLocal_WriteReadURLDelete(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root, URL: "http://localhost:8080/static"})
	require.NoError(t, err)
	ctx := context.Background()

	// Write into a nested subdirectory
	err = s.Write(ctx, "images/a.png", []byte("png-data"))
	require.NoError(t, err)

	// The file really lands on disk
	got, err := os.ReadFile(filepath.Join(root, "images", "a.png"))
	require.NoError(t, err)
	assert.Equal(t, "png-data", string(got))

	// URL uses the configured prefix
	u, err := s.URL(ctx, "images/a.png")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/static/images/a.png", u)

	// Overwrite an existing file
	err = s.Write(ctx, "images/a.png", []byte("new"))
	require.NoError(t, err)
	got, err = os.ReadFile(filepath.Join(root, "images", "a.png"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	// Delete
	n, err := s.Delete(ctx, "images/a.png")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, err = os.Stat(filepath.Join(root, "images", "a.png"))
	assert.True(t, os.IsNotExist(err))
}

func TestLocal_DeleteNotExist(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	n, err := s.Delete(context.Background(), "not/exist.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestLocal_URLFileScheme(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	// Without a URL prefix a file:// absolute path is returned
	u, err := s.URL(context.Background(), "dir/f.txt")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "file://"))
	assert.True(t, strings.HasSuffix(u, "dir/f.txt"))
}

// --- Extra: local error branches ---

func TestLocal_WriteCancelledCtx(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Write(ctx, "a.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write local file")
}

func TestLocal_DeleteCancelledCtx(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Delete(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete local file")
}

func TestLocal_WriteDirBlockedByFile(t *testing.T) {
	// The parent path is taken by a file of the same name -> MkdirAll fails
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	blocker := filepath.Join(root, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("file"), 0o644))

	// The parent directory blocker of blocker/x.txt is a file -> MkdirAll errors
	err = s.Write(context.Background(), "blocker/x.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create local directory")
}

func TestLocal_DeleteDirectory(t *testing.T) {
	// Calling os.Remove on a non-empty directory reports an error
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	dir := filepath.Join(root, "adir")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755)) // non-empty

	_, err = s.Delete(context.Background(), "adir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete local file")
}

// --- Read / Exists ---

func TestLocal_ReadExists(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)
	ctx := context.Background()

	// Before writing: does not exist
	ok, err := s.Exists(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.False(t, ok)

	// After writing: exists and the content reads back
	require.NoError(t, s.Write(ctx, "docs/a.txt", []byte("hello")))
	ok, err = s.Exists(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.True(t, ok)

	data, err := s.Read(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	// After deleting: gone again, and reading returns an error
	_, err = s.Delete(ctx, "docs/a.txt")
	require.NoError(t, err)
	ok, err = s.Exists(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestLocal_ReadNotExist(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	_, err = s.Read(context.Background(), "not/exist.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read local file")
}

func TestLocal_ReadCancelledCtx(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Read(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read local file")
}

func TestLocal_ExistsCancelledCtx(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Exists(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check local file")
}

// --- Security: path traversal protection ---

// TestLocal_PathTraversalRejected verifies that any path escaping root is
// rejected and that no file outside root is read / written / deleted.
func TestLocal_PathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	// Place a "sensitive file" outside root to verify it cannot be reached or
	// deleted through a path traversal.
	outside := filepath.Join(filepath.Dir(root), "infra-go-secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	t.Cleanup(func() { _ = os.Remove(outside) })

	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)
	ctx := context.Background()

	escapes := []string{
		"../infra-go-secret.txt",
		"../../infra-go-secret.txt",
		"a/../../infra-go-secret.txt", // walks back in the middle
		"/../../infra-go-secret.txt",  // leading slash plus walk-back
		"//../infra-go-secret.txt",    // a double slash prefix must not bypass the check
		"..",                          // points at the parent directory itself
		"../",                         // parent directory (with a trailing slash)
	}
	for _, p := range escapes {
		t.Run(p, func(t *testing.T) {
			_, err := s.Read(ctx, p)
			require.ErrorIs(t, err, errPathEscapesRoot, "Read(%q)", p)

			err = s.Write(ctx, p, []byte("pwned"))
			require.ErrorIs(t, err, errPathEscapesRoot, "Write(%q)", p)

			_, err = s.Exists(ctx, p)
			require.ErrorIs(t, err, errPathEscapesRoot, "Exists(%q)", p)

			_, err = s.Delete(ctx, p)
			require.ErrorIs(t, err, errPathEscapesRoot, "Delete(%q)", p)

			_, err = s.URL(ctx, p)
			require.ErrorIs(t, err, errPathEscapesRoot, "URL(%q)", p)
		})
	}

	// The sensitive file was neither deleted nor overwritten.
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "secret", string(data))

	// No new file appeared outside root because of a traversal write.
	_, err = os.Stat(filepath.Join(filepath.Dir(root), "pwned"))
	assert.True(t, os.IsNotExist(err))
}

// TestLocal_EmptyPathRejected verifies that an empty path is rejected, so the
// root directory itself cannot be touched by mistake.
func TestLocal_EmptyPathRejected(t *testing.T) {
	s, err := NewLocal(&LocalConfig{RootDir: t.TempDir()})
	require.NoError(t, err)
	ctx := context.Background()

	for _, p := range []string{"", "/", "///"} {
		_, err := s.Read(ctx, p)
		require.ErrorIs(t, err, errPathEscapesRoot, "Read(%q)", p)
		require.ErrorIs(t, s.Write(ctx, p, []byte("x")), errPathEscapesRoot, "Write(%q)", p)
		_, err = s.Exists(ctx, p)
		require.ErrorIs(t, err, errPathEscapesRoot, "Exists(%q)", p)
		_, err = s.Delete(ctx, p)
		require.ErrorIs(t, err, errPathEscapesRoot, "Delete(%q)", p)
		_, err = s.URL(ctx, p)
		require.ErrorIs(t, err, errPathEscapesRoot, "URL(%q)", p)
	}
}

// TestLocal_SafePathNormalization verifies that valid relative paths (with a
// leading "/", ".", or an internally cleanable "..") still work and all stay
// below root, so fixing the traversal does not break compatibility.
func TestLocal_SafePathNormalization(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)
	ctx := context.Background()

	paths := []string{"a/b.txt", "/a/b.txt", "./a/b.txt", "a/./b.txt", "a/x/../b.txt"}
	for _, p := range paths {
		require.NoError(t, s.Write(ctx, p, []byte("ok")), "Write(%q)", p)
		data, err := s.Read(ctx, p)
		require.NoError(t, err, "Read(%q)", p)
		assert.Equal(t, "ok", string(data), "Read(%q)", p)
		ok, err := s.Exists(ctx, p)
		require.NoError(t, err, "Exists(%q)", p)
		assert.True(t, ok, "Exists(%q)", p)
	}

	// All of them normalize to root/a/b.txt
	got, err := os.ReadFile(filepath.Join(root, "a", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "ok", string(got))
}

// TestLocal_DeleteTraversalDoesNotRemoveDirectory verifies that a traversal
// delete cannot hit root itself or its parent directory.
func TestLocal_DeleteTraversalDoesNotRemoveDirectory(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	_, err = s.Delete(context.Background(), "..")
	require.ErrorIs(t, err, errPathEscapesRoot)

	// The root directory still exists
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
