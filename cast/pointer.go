package cast

import "time"

// --- Generic address-of / dereference ---

// Ptr returns a pointer to v.
// Go does not allow taking the address of a literal or constant directly, so Ptr is a
// convenient way to wrap a value in a pointer; it is commonly used to give optional
// pointer fields a default value:
//
//	name := cast.Ptr("default")   // *string
//	limit := cast.Ptr(10)          // *int
func Ptr[T any](v T) *T {
	return &v
}

// Val dereferences the pointer p safely.
// When p is nil it returns the default supplied via def, and when def is not supplied it
// returns the zero value of the type.
//
//	n := cast.Val(pInt, 0)   // yields 0 when pInt is nil
//	s := cast.Val(pStr)      // yields "" when pStr is nil
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

// --- Type-specific conversions (any → *T, nil if the conversion fails) ---

// ToIntPtr converts any to *int; returns nil if the conversion fails.
func ToIntPtr(v any) *int {
	n, err := ToIntE(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToInt64Ptr converts any to *int64; returns nil if the conversion fails.
func ToInt64Ptr(v any) *int64 {
	n, err := ToInt64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToUintPtr converts any to *uint; returns nil if the conversion fails.
func ToUintPtr(v any) *uint {
	n, err := ToUintE(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToUint64Ptr converts any to *uint64; returns nil if the conversion fails.
func ToUint64Ptr(v any) *uint64 {
	n, err := ToUint64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToFloat32Ptr converts any to *float32; returns nil if the conversion fails.
func ToFloat32Ptr(v any) *float32 {
	n, err := ToFloat32E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToFloat64Ptr converts any to *float64; returns nil if the conversion fails.
func ToFloat64Ptr(v any) *float64 {
	n, err := ToFloat64E(v)
	if err != nil {
		return nil
	}
	return &n
}

// ToStringPtr converts any to *string; returns nil if the conversion fails.
func ToStringPtr(v any) *string {
	s, err := ToStringE(v)
	if err != nil {
		return nil
	}
	return &s
}

// ToBoolPtr converts any to *bool; returns nil if the conversion fails.
func ToBoolPtr(v any) *bool {
	b, err := ToBoolE(v)
	if err != nil {
		return nil
	}
	return &b
}

// ToDurationPtr converts any to *time.Duration; returns nil if the conversion fails.
func ToDurationPtr(v any) *time.Duration {
	d, err := ToDurationE(v)
	if err != nil {
		return nil
	}
	return &d
}

// ToTimePtr converts any to *time.Time; returns nil if the conversion fails.
func ToTimePtr(v any) *time.Time {
	tm, err := ToTimeE(v)
	if err != nil {
		return nil
	}
	return &tm
}
