package trace

import (
	"os"

	"github.com/chihqiang/infra-go/mapping"
	"go.opentelemetry.io/otel/attribute"
)

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewDefaultUnmarshaler()

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖。
func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	// 用用户配置中的非零字段覆盖默认值
	if cfg.Name != "" {
		c.Name = cfg.Name
	}
	// Endpoint：空字符串也是有效值
	c.Endpoint = cfg.Endpoint
	if cfg.Sampler > 0 {
		c.Sampler = cfg.Sampler
	}
	if cfg.Batcher != "" {
		c.Batcher = cfg.Batcher
	}
	if len(cfg.OtlpHeaders) > 0 {
		c.OtlpHeaders = cfg.OtlpHeaders
	}
	if cfg.OtlpHttpPath != "" {
		c.OtlpHttpPath = cfg.OtlpHttpPath
	}
	if cfg.OtlpHttpSecure {
		c.OtlpHttpSecure = cfg.OtlpHttpSecure
	}
	if cfg.OtlpGrpcSecure {
		c.OtlpGrpcSecure = cfg.OtlpGrpcSecure
	}
	if cfg.Disabled {
		c.Disabled = cfg.Disabled
	}

	return c
}

// --- 资源管理 ---

var attrResources = make([]attribute.KeyValue, 0)

// AddResources 添加额外的资源属性。
// 资源属性会附加到所有链路 span 上，用于标识服务来源。
// 使用 AttrString / AttrInt 等函数创建属性，无需导入 otel/attribute。
func AddResources(attrs ...Attr) {
	attrResources = append(attrResources, attrs...)
}

// resetResources 重置资源属性（仅用于测试）。
func resetResources() {
	attrResources = make([]attribute.KeyValue, 0)
}

// ensureFile 确保 trace 日志文件的写入器在测试后可关闭。
// 当 Batcher 为 file 时，需要持有文件句柄以便后续关闭。
var fileCloser func()

// closeFile 关闭 trace 日志文件（仅用于测试）。
func closeFile() {
	if fileCloser != nil {
		fileCloser()
		fileCloser = nil
	}
}

// openFileForExporter 打开文件用于 file 类型导出器。
func openFileForExporter(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fileCloser = func() { _ = f.Close() }
	return f, nil
}
