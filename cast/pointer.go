package cast

import "time"

// --- 泛型取址 / 解引用 ---

// Ptr 返回 v 的指针。
// Go 不允许直接对字面量/常量取址，Ptr 可方便地把值包装成指针，
// 常用于给可选指针字段赋默认值：
//
//	name := cast.Ptr("default")   // *string
//	limit := cast.Ptr(10)          // *int
func Ptr[T any](v T) *T {
	return &v
}

// Val 安全解引用指针 p。
// p 为 nil 时返回 def 提供的默认值；未提供 def 时返回该类型的零值。
//
//	n := cast.Val(pInt, 0)   // pInt 为 nil 时得到 0
//	s := cast.Val(pStr)      // pStr 为 nil 时得到 ""
func Val[T any](p *T, def ...T) T {
	if p == nil {
		if len(def) > 0 {
			return def[0]
		}
		var zero T
		return zero
	}
	return *p
}

// --- 类型专属转换（any → *T，转换失败返回 nil） ---

// ToIntPtr 将 any 转换为 *int；转换失败返回 nil。
func ToIntPtr(v any) *int {
	n, err := ToIntE(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToInt64Ptr 将 any 转换为 *int64；转换失败返回 nil。
func ToInt64Ptr(v any) *int64 {
	n, err := ToInt64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToUintPtr 将 any 转换为 *uint；转换失败返回 nil。
func ToUintPtr(v any) *uint {
	n, err := ToUintE(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToUint64Ptr 将 any 转换为 *uint64；转换失败返回 nil。
func ToUint64Ptr(v any) *uint64 {
	n, err := ToUint64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToFloat32Ptr 将 any 转换为 *float32；转换失败返回 nil。
func ToFloat32Ptr(v any) *float32 {
	n, err := ToFloat32E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToFloat64Ptr 将 any 转换为 *float64；转换失败返回 nil。
func ToFloat64Ptr(v any) *float64 {
	n, err := ToFloat64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToStringPtr 将 any 转换为 *string；转换失败返回 nil。
func ToStringPtr(v any) *string {
	s, err := ToStringE(v)
	if err != nil {
		return nil
	}
	return &s
}

// ToBoolPtr 将 any 转换为 *bool；转换失败返回 nil。
func ToBoolPtr(v any) *bool {
	b, err := ToBoolE(v)
	if err != nil {
		return nil
	}
	return &b
}

// ToDurationPtr 将 any 转换为 *time.Duration；转换失败返回 nil。
func ToDurationPtr(v any) *time.Duration {
	d, err := ToDurationE(v)
	if err != nil {
		return nil
	}
	return &d
}

// ToTimePtr 将 any 转换为 *time.Time；转换失败返回 nil。
func ToTimePtr(v any) *time.Time {
	tm, err := ToTimeE(v)
	if err != nil {
		return nil
	}
	return &tm
}
