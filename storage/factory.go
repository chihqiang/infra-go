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
