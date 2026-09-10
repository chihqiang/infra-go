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

// --- Read / Exists ---

func TestLocal_ReadExists(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)
	ctx := context.Background()

	// 未写入前：不存在
	ok, err := s.Exists(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.False(t, ok)

	// 写入后：存在且内容可读回
	require.NoError(t, s.Write(ctx, "docs/a.txt", []byte("hello")))
	ok, err = s.Exists(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.True(t, ok)

	data, err := s.Read(ctx, "docs/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	// 删除后：恢复为不存在，读取报错
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

// --- 安全：路径穿越防护 ---

// TestLocal_PathTraversalRejected 验证任何逃逸 root 的 path 都被拒绝，
// 且不会读取 / 写入 / 删除 root 之外的文件。
func TestLocal_PathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	// 在 root 之外放置"敏感文件"，用于验证无法经由路径穿越访问或删除。
	outside := filepath.Join(filepath.Dir(root), "infra-go-secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	t.Cleanup(func() { _ = os.Remove(outside) })

	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)
	ctx := context.Background()

	escapes := []string{
		"../infra-go-secret.txt",
		"../../infra-go-secret.txt",
		"a/../../infra-go-secret.txt", // 中途回溯
		"/../../infra-go-secret.txt",  // 前导斜杠 + 回溯
		"//../infra-go-secret.txt",    // 双斜杠前缀不得被绕过
		"..",                          // 指向父目录本身
		"../",                         // 父目录（带尾斜杠）
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

	// 敏感文件既未被删除也未被覆盖。
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "secret", string(data))

	// root 之外没有因穿越写入而新增文件。
	_, err = os.Stat(filepath.Join(filepath.Dir(root), "pwned"))
	assert.True(t, os.IsNotExist(err))
}

// TestLocal_EmptyPathRejected 验证空路径被拒绝，避免误操作 root 目录本身。
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

// TestLocal_SafePathNormalization 验证合法的相对路径（含前导 "/"、"." 与
// 内部可归一化的 ".."）仍然可用，且都落在 root 之内，避免修复穿越时误伤兼容性。
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

	// 全部归一化到 root/a/b.txt
	got, err := os.ReadFile(filepath.Join(root, "a", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "ok", string(got))
}

// TestLocal_DeleteTraversalDoesNotRemoveDirectory 验证穿越删除不会命中 root 自身或其父目录。
func TestLocal_DeleteTraversalDoesNotRemoveDirectory(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(&LocalConfig{RootDir: root})
	require.NoError(t, err)

	_, err = s.Delete(context.Background(), "..")
	require.ErrorIs(t, err, errPathEscapesRoot)

	// root 目录仍然存在
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
