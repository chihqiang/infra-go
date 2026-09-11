package trace

import (
	"os"
	"sync"

	"github.com/chihqiang/infra-go/mapping"
	"go.opentelemetry.io/otel/attribute"
)

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewDefaultUnmarshaler()

// fillDefault 填充默认值，然后用用户配置中的非零字段覆盖，最后应用 opts。
//
// 局限：非零字段覆盖的规则无法区分"未设置"与"显式设为 0"，
// 需要表达显式 0（如 Sampler = 0）时通过 opts 传入，见 Option。
func fillDefault(cfg Config, opts ...Option) Config {
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

	// Option 最后应用：可覆盖上面「零值即未设置」的判定结果。
	for _, opt := range opts {
		opt(&c)
	}

	return c
}

// --- 资源管理 ---

var (
	// attrResources 会附加到所有 span 的 Resource 上。
	//
	// 用锁保护：AddResources 可能被业务在运行期调用（动态打标签），
	// 而 startAgent 会读取它构造 Resource；无保护时 -race 可检出数据竞争。
	attrResourcesLk sync.RWMutex
	attrResources   = make([]attribute.KeyValue, 0)
)

// AddResources 添加额外的资源属性。
// 资源属性会附加到所有链路 span 上，用于标识服务来源。
// 使用 AttrString / AttrInt 等函数创建属性，无需导入 otel/attribute。
//
// 并发安全。注意：属性是在 TracerProvider 创建时快照的，
// 因此 **启动后添加的属性不会生效**（需要重新 StartAgent）。
func AddResources(attrs ...Attr) {
	if len(attrs) == 0 {
		return
	}
	attrResourcesLk.Lock()
	attrResources = append(attrResources, attrs...)
	attrResourcesLk.Unlock()
}

// resourceAttrs 返回资源属性副本，供构造 Resource 使用。
func resourceAttrs() []attribute.KeyValue {
	attrResourcesLk.RLock()
	defer attrResourcesLk.RUnlock()
	out := make([]attribute.KeyValue, len(attrResources))
	copy(out, attrResources)
	return out
}

// resetResources 重置资源属性（仅用于测试）。
func resetResources() {
	attrResourcesLk.Lock()
	attrResources = make([]attribute.KeyValue, 0)
	attrResourcesLk.Unlock()
}

// openFileForExporter 打开文件用于 file 类型导出器。
// 返回文件及其关闭函数：调用方负责在 agent 停止时关闭，
// 避免把 closer 存在包级变量里（多实例会互相覆盖，且 StopAgent 无法释放）。
func openFileForExporter(path string) (*os.File, func() error, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
