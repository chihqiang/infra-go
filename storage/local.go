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

// localStorage 本地文件系统存储实现。
// 将文件写入本地磁盘目录，适合开发/单机场景或作为云存储的本地替代。
type localStorage struct {
	root string // 本地根目录（绝对或相对路径）
	url  string // 访问 URL 前缀（可选，如 http://localhost:8080/static），空则返回 file:// URL
}

// NewLocal 根据配置创建本地文件系统存储实例。
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

// errPathEscapesRoot 表示传入的 path 会逃出存储根目录。
var errPathEscapesRoot = errors.New("path escapes root directory")

// safePath 将存储 path（key）转换为 root 之下的本地绝对文件路径。
//
// 安全约束：path 可能来自不可信输入（上传文件名、用户指定的 key），
// 因此必须拒绝任何逃逸 root 的路径，否则 filepath.Join 会 Clean 掉 ".."
// 从而允许任意文件读 / 写 / 删（如 path="../../etc/passwd"）。
//
// 被拒绝的形式：空路径、绝对路径、包含 ".." 回溯出 root 的路径。
// 前导 "/" 会被裁剪（与 buildURL 的语义保持一致），因此 "/a/b.txt"
// 等价于 "a/b.txt"，仍落在 root 之内。
//
// 注意：本函数只做词法校验，不解析符号链接。若 root 目录内存在指向
// root 之外的符号链接，仍可被间接访问；需要防此类攻击时应对 root 目录
// 的写入权限做管控，避免不可信方在 root 内创建符号链接。
func (s *localStorage) safePath(path string) (string, error) {
	rel := filepath.FromSlash(strings.TrimLeft(filepath.ToSlash(path), "/"))
	if rel == "" {
		return "", fmt.Errorf("%w: path is empty", errPathEscapesRoot)
	}
	// IsLocal 拒绝绝对路径与逃出当前目录的 ".." 回溯。
	if !filepath.IsLocal(rel) {
		return "", errPathEscapesRoot
	}
	full := filepath.Join(s.root, rel)
	// 双保险：Join 已做 Clean，此处再确认结果确实位于 root 之下。
	if full != s.root && !strings.HasPrefix(full, s.root+string(os.PathSeparator)) {
		return "", errPathEscapesRoot
	}
	return full, nil
}

// Write 将内容写入本地文件系统指定路径。
// 自动创建路径所需的父目录；已存在的同名文件会被覆盖。
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

// Read 读取本地文件系统指定路径的文件内容。
// 文件不存在时返回错误（底层 os.ReadFile 的 *PathError）。
// path 逃逸根目录时返回错误（不触碰文件系统）。
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

// Exists 判断本地文件系统指定路径的文件是否存在。
// path 逃逸根目录时返回错误（视为不存在）。
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

// Delete 删除本地文件系统指定路径的文件，返回删除的文件数量。
// 文件不存在视为已删除（返回 0），不报错。
// path 逃逸根目录时返回错误（不删除任何文件）。
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

// URL 生成本地文件的访问 URL。
// 若配置了 URL 前缀则拼接为该前缀下的地址（常见于挂载静态文件服务）；
// 否则返回 file:// 协议的本地绝对路径。
// path 逃逸根目录时返回错误。
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
