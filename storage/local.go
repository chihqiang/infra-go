package storage

import (
	"context"
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

// localPath 将存储 path（key）转换为本地绝对文件路径。
func (s *localStorage) localPath(path string) string {
	return filepath.Join(s.root, filepath.FromSlash(path))
}

// Write 将内容写入本地文件系统指定路径。
// 自动创建路径所需的父目录；已存在的同名文件会被覆盖。
func (s *localStorage) Write(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: write local file %q: %w", path, err)
	}
	full := s.localPath(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("storage: failed to create local directory for %q: %w", path, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("storage: failed to write local file %q: %w", path, err)
	}
	return nil
}

// Delete 删除本地文件系统指定路径的文件，返回删除的文件数量。
// 文件不存在视为已删除（返回 0），不报错。
func (s *localStorage) Delete(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete local file %q: %w", path, err)
	}
	full := s.localPath(path)
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
func (s *localStorage) URL(_ context.Context, path string) (string, error) {
	if s.url != "" {
		return buildURL(s.url, path)
	}
	full := s.localPath(path)
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(full)}).String(), nil
}
