package syncx

import "sync"

// OnceValue is the generic counterpart of sync.OnceValue.
// The function is guaranteed to run only once; later calls return the cached
// result. If load panics, the panic propagates to the caller and the failed
// result is not cached.
//
// Usage:
//
//	var config = syncx.NewOnceValue(func() *Config {
//	    return loadConfig()
//	})
//	cfg := config.Get() // loads once; later calls return the cached value
type OnceValue[T any] struct {
	once     sync.Once
	value    T
	panicVal any // records the value of a load panic, re-panicked on later calls
	load     func() T
}

// NewOnceValue creates a value loader that runs at most once.
func NewOnceValue[T any](load func() T) *OnceValue[T] {
	return &OnceValue[T]{load: load}
}

// Get returns the value. The first call runs load; later calls return the cached
// value directly. If load panics, the panic propagates to the caller (matching
// the standard sync.OnceValue).
func (o *OnceValue[T]) Get() T {
	o.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				o.panicVal = r
			}
		}()
		o.value = o.load()
	})
	if o.panicVal != nil {
		panic(o.panicVal)
	}
	return o.value
}

// OnceError is a generic run-at-most-once loader that also carries an error.
// It is meant for lazily loading and caching an operation that may fail.
// If load panics, the panic propagates to the caller and the failed result is
// not cached.
//
// Usage:
//
//	var conn = syncx.NewOnceError(func() (*sql.DB, error) {
//	    return sql.Open("mysql", dsn)
//	})
//	db, err := conn.Get()
type OnceError[T any] struct {
	once     sync.Once
	value    T
	err      error
	panicVal any // records the value of a load panic, re-panicked on later calls
	load     func() (T, error)
}

// NewOnceError creates a value loader that runs at most once and returns an error.
func NewOnceError[T any](load func() (T, error)) *OnceError[T] {
	return &OnceError[T]{load: load}
}

// Get returns the value and error. The first call runs load; later calls return
// the cached result directly. If load panics, the panic propagates to the caller
// (matching the standard sync.OnceValue).
func (o *OnceError[T]) Get() (T, error) {
	o.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				o.panicVal = r
			}
		}()
		o.value, o.err = o.load()
	})
	if o.panicVal != nil {
		panic(o.panicVal)
	}
	return o.value, o.err
}
