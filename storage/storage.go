package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Storage 存储服务接口，定义了对象存储的基本操作。
// 目前支持本地文件系统、阿里云 OSS、腾讯云 COS 和七牛云 KODO 四种实现。
//
// 注意：阿里云 OSS SDK 不支持 context 取消，传给 Write/Delete 的 context
// 在 OSS 实现中仅用于超时检测（通过 context-aware reader），不会中断 SDK 调用。
type Storage interface {
	// Write 将内容写入指定路径。
	// ctx 用于控制请求超时和取消。
	// path 为对象在存储桶中的路径（key），content 为文件内容。
	Write(ctx context.Context, path string, content []byte) error

	// Delete 删除指定路径的对象，返回删除的对象数量。
	// ctx 用于控制请求超时和取消。
	// path 为对象在存储桶中的路径（key）。
	Delete(ctx context.Context, path string) (int64, error)

	// URL 根据路径拼接完整的访问 URL。
	// ctx 保留用于未来扩展（目前所有实现为本地 URL 拼接，不依赖 context）。
	// path 为对象在存储桶中的路径（key）。
	URL(ctx context.Context, path string) (string, error)
}

// buildURL 将基础域名和路径拼接为完整的 URL。
// 自动处理 base 尾部和 path 头部的斜杠，并对 path 进行 URL 编码。
func buildURL(base, path string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("storage: base URL is empty, please set URL field in config")
	}
	base = strings.TrimRight(base, "/")
	path = strings.TrimLeft(path, "/")
	// 对 path 中的特殊字符进行 URL 编码（保留 / 分隔符）
	encoded := (&url.URL{Path: path}).String()
	return base + "/" + encoded, nil
}

// Driver 存储驱动类型。
type Driver string

const (
	// DriverOSS 阿里云 OSS 存储。
	DriverOSS Driver = "oss"
	// DriverCOS 腾讯云 COS 存储。
	DriverCOS Driver = "cos"
	// DriverKODO 七牛云 KODO 存储。
	DriverKODO Driver = "kodo"
	// DriverLocal 本地文件系统存储。
	DriverLocal Driver = "local"
)

// Storages 存储实例集合，key 为实例别名（如 "images"、"docs"），
// value 为已实例化的 Storage 实现。一套凭证或多种驱动可按别名共存。
type Storages map[string]Storage

// Get 按别名获取存储实例。
func (s Storages) Get(name string) (Storage, bool) {
	st, ok := s[name]
	return st, ok
}

// Write 将内容写入指定别名的存储实例。
// name 为存储实例别名，path 为对象在存储中的路径（key）。
func (s Storages) Write(ctx context.Context, name, path string, content []byte) error {
	st, ok := s[name]
	if !ok {
		return fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Write(ctx, path, content)
}

// Delete 删除指定别名的存储实例中的对象，返回删除的对象数量。
func (s Storages) Delete(ctx context.Context, name, path string) (int64, error) {
	st, ok := s[name]
	if !ok {
		return 0, fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.Delete(ctx, path)
}

// URL 根据路径拼接指定别名的存储实例的完整访问 URL。
func (s Storages) URL(ctx context.Context, name, path string) (string, error) {
	st, ok := s[name]
	if !ok {
		return "", fmt.Errorf("storage: unknown storage %q", name)
	}
	return st.URL(ctx, path)
}
