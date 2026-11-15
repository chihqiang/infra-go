package cast

import (
	"encoding/json"
	"time"
)

// --- Generic conversion ---

// To converts any to the target type T.
// T may be a basic type or a pointer to one; a failed conversion returns the zero value.
// Use ToE when you need to know whether the conversion succeeded.
//
// Usage:
//
//	n := cast.To[int]("123")       // 123
//	s := cast.To[string](456)      // "456"
//	b := cast.To[bool]("true")     // true
//	d := cast.To[time.Duration]("5s") // 5s
func To[T any](v any) T {
	val, _ := ToE[T](v)
	return val
}

// narrowInt64E converts v to int64 and verifies that it fits losslessly into a signed
// integer of bitSize bits.
// Used by the int8/int16/int32 branches of ToE: a narrowing conversion such as int8(n)
// wraps around silently (e.g. int8(200) == -56), so the range must be checked first.
func narrowInt64E(v any, bitSize int, name string) (int64, error) {
	n, err := ToInt64E(v)
	if err != nil {
		return 0, err
	}
	if err := checkIntBitSize(n, bitSize, name); err != nil {
		return 0, err
	}
	return n, nil
}

// narrowUint64E converts v to uint64 and verifies that it fits losslessly into an
// unsigned integer of bitSize bits.
func narrowUint64E(v any, bitSize int, name string) (uint64, error) {
	n, err := ToUint64E(v)
	if err != nil {
		return 0, err
	}
	if err := checkUintBitSize(n, bitSize, name); err != nil {
		return 0, err
	}
	return n, nil
}

// ToE converts any to the target type T and returns the result along with an error.
// Unlike To it preserves the error, so callers can detect a failure and fall back to a default.
func ToE[T any](v any) (T, error) {
	var zero T
	switch any(zero).(type) {
	case int:
		n, err := ToIntE(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case int8:
		n, err := narrowInt64E(v, 8, "int8")
		if err != nil {
			return zero, err
		}
		return any(int8(n)).(T), nil
	case int16:
		n, err := narrowInt64E(v, 16, "int16")
		if err != nil {
			return zero, err
		}
		return any(int16(n)).(T), nil
	case int32:
		n, err := narrowInt64E(v, 32, "int32")
		if err != nil {
			return zero, err
		}
		return any(int32(n)).(T), nil
	case int64:
		n, err := ToInt64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case uint:
		n, err := ToUintE(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case uint8:
		n, err := narrowUint64E(v, 8, "uint8")
		if err != nil {
			return zero, err
		}
		return any(uint8(n)).(T), nil
	case uint16:
		n, err := narrowUint64E(v, 16, "uint16")
		if err != nil {
			return zero, err
		}
		return any(uint16(n)).(T), nil
	case uint32:
		n, err := narrowUint64E(v, 32, "uint32")
		if err != nil {
			return zero, err
		}
		return any(uint32(n)).(T), nil
	case uint64:
		n, err := ToUint64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case float32:
		n, err := ToFloat32E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case float64:
		n, err := ToFloat64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case string:
		s, err := ToStringE(v)
		if err != nil {
			return zero, err
		}
		return any(s).(T), nil
	case bool:
		b, err := ToBoolE(v)
		if err != nil {
			return zero, err
		}
		return any(b).(T), nil
	case time.Duration:
		d, err := ToDurationE(v)
		if err != nil {
			return zero, err
		}
		return any(d).(T), nil
	case time.Time:
		t, err := ToTimeE(v)
		if err != nil {
			return zero, err
		}
		return any(t).(T), nil
	default:
		// fall back to JSON marshal/unmarshal
		if v == nil {
			return zero, nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return zero, err
		}
		var result T
		if err := json.Unmarshal(b, &result); err != nil {
			return zero, err
		}
		return result, nil
	}
}
