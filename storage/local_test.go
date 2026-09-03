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
