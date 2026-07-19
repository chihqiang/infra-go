package syncx

import "sync"

// OnceValue 泛型版本的 sync.OnceValue。
// 保证函数只执行一次，后续调用返回缓存的结果。
//
// 用法：
//
//	var config = syncx.NewOnceValue(func() *Config {
//	    return loadConfig()
//	})
//	cfg := config.Get() // 只加载一次，后续直接返回缓存
type OnceValue[T any] struct {
	once  sync.Once
	value T
	load  func() T
}

// NewOnceValue 创建一个只执行一次的值加载器。
func NewOnceValue[T any](load func() T) *OnceValue[T] {
	return &OnceValue[T]{load: load}
}

// Get 返回值，首次调用会执行 load 函数，后续调用直接返回缓存值。
func (o *OnceValue[T]) Get() T {
	o.once.Do(func() {
		o.value = o.load()
	})
	return o.value
}

// OnceError 泛型版本的只执行一次的 error 加载器。
// 用于懒加载并缓存可能失败的操作。
//
// 用法：
//
//	var conn = syncx.NewOnceError(func() (*sql.DB, error) {
//	    return sql.Open("mysql", dsn)
//	})
//	db, err := conn.Get()
type OnceError[T any] struct {
	once  sync.Once
	value T
	err   error
	load  func() (T, error)
}

// NewOnceError 创建一个只执行一次的值加载器（带 error）。
func NewOnceError[T any](load func() (T, error)) *OnceError[T] {
	return &OnceError[T]{load: load}
}

// Get 返回值和错误，首次调用会执行 load 函数，后续调用直接返回缓存结果。
func (o *OnceError[T]) Get() (T, error) {
	o.once.Do(func() {
		o.value, o.err = o.load()
	})
	return o.value, o.err
}
