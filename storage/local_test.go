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

	// Write 到嵌套子目录
	err = s.Write(ctx, "images/a.png", []byte("png-data"))
	require.NoError(t, err)

	// 文件真实落盘
	got, err := os.ReadFile(filepath.Join(root, "images", "a.png"))
	require.NoError(t, err)
	assert.Equal(t, "png-data", string(got))

	// URL 使用配置前缀
	u, err := s.URL(ctx, "images/a.png")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/static/images/a.png", u)

	// 覆盖已存在文件
	err = s.Write(ctx, "images/a.png", []byte("new"))
	require.NoError(t, err)
	got, err = os.ReadFile(filepath.Join(root, "images", "a.png"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	// 删除
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

	// 无 URL 前缀时返回 file:// 绝对路径
	u, err := s.URL(context.Background(), "dir/f.txt")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "file://"))
	assert.True(t, strings.HasSuffix(u, "dir/f.txt"))
}

// --- 补充：local 错误分支 ---

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
	// 父路径被同名文件占据 → MkdirAll 失败
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	blocker := filepath.Join(root, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("file"), 0o644))

	// blocker/x.txt 的父目录 blocker 是个文件 → MkdirAll 报错
	err = s.Write(context.Background(), "blocker/x.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create local directory")
}

func TestLocal_DeleteDirectory(t *testing.T) {
	// 对非空目录调用 os.Remove 会报错
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	dir := filepath.Join(root, "adir")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755)) // 非空

	_, err = s.Delete(context.Background(), "adir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete local file")
}
