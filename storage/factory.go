package storage

import "fmt"

// New 根据配置创建存储实例。
// 工厂方法：根据 Config.Driver 选择对应的存储实现（OSS、COS 或 KODO）。
func New(cfg Config) (Storage, error) {
	switch cfg.Driver {
	case DriverOSS:
		return NewOSS(cfg.OSS)
	case DriverCOS:
		return NewCOS(cfg.COS)
	case DriverKODO:
		return NewKODO(cfg.KODO)
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q, supported: oss, cos, kodo", cfg.Driver)
	}
}

// MustNew 根据配置创建存储实例，出错时 panic。
func MustNew(cfg Config) Storage {
	s, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("storage: failed to create storage: %w", err))
	}
	return s
}

// NewStorages 注入已实例化的 Storage 实例，按别名组织为存储实例集合。
// 每个 Storage 实例绑定一个物理桶（由 NewOSS/NewCOS/NewKODO 创建），
// key 为实例别名，支持一个集合管理多个存储桶、每种驱动独立配置。
func NewStorages(storages map[string]Storage) Storages {
	return Storages(storages)
}
