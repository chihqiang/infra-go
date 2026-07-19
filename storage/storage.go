package storage

import (
	"fmt"
	"net/url"
	"strings"
)

// Storage 存储服务接口，定义了对象存储的基本操作。
// 目前支持阿里云 OSS、腾讯云 COS 和七牛云 KODO 三种实现。
type Storage interface {
	// Write 将内容写入指定路径。
	// path 为对象在存储桶中的路径（key），content 为文件内容。
	Write(path string, content []byte) error

	// Delete 删除指定路径的对象，返回删除的对象数量。
	// path 为对象在存储桶中的路径（key）。
	Delete(path string) (int64, error)

	// URL 根据路径拼接完整的访问 URL。
	// path 为对象在存储桶中的路径（key）。
	URL(path string) (string, error)
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
)
