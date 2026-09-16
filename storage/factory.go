package storage

import "fmt"

// New creates a storage instance from the configuration.
// Factory method: it picks the matching storage implementation based on
// Config.Driver (local, OSS, COS or KODO).
func New(cfg Config) (Storage, error) {
	switch cfg.Driver {
	case DriverLocal:
		return NewLocal(cfg.Local)
	case DriverOSS:
		return NewOSS(cfg.OSS)
	case DriverCOS:
		return NewCOS(cfg.COS)
	case DriverKODO:
		return NewKODO(cfg.KODO)
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q, supported: local, oss, cos, kodo", cfg.Driver)
	}
}

// MustNew creates a storage instance from the configuration and panics on failure.
func MustNew(cfg Config) Storage {
	s, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("storage: failed to create storage: %w", err))
	}
	return s
}
