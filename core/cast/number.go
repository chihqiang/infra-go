package cast

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// --- Integer conversion (int) ---

// ToInt converts any to int, returning the zero value if the conversion fails.
// Supports the int family, the float family, string, bool and json.Number.
func ToInt(v any) int {
	val, _ := ToIntE(v)
	return val
}

// ToInt64 converts any to int64, returning the zero value if the conversion fails.
func ToInt64(v any) int64 {
	val, _ := ToInt64E(v)
	return val
}

// ToIntE converts any to int and returns the result along with an error.
func ToIntE(v any) (int, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return val, nil
	case int8:
		return int(val), nil
	case int16:
		return int(val), nil
	case int32:
		return int(val), nil
	case int64:
		return int(val), nil
	case uint:
		// prevent a silent overflow to a negative value when the input exceeds math.MaxInt
		if uint64(val) > uint64(math.MaxInt) {
			return 0, castErr("uint", "int")
		}
		return int(val), nil
	case uint8:
		return int(val), nil
	case uint16:
		return int(val), nil
	case uint32:
		return int(val), nil
	case uint64:
		if val > uint64(math.MaxInt) {
			return 0, castErr("uint64", "int")
		}
		return int(val), nil
	case float32:
		return floatToIntE(float64(val), "float32")
	case float64:
		return floatToIntE(val, "float64")
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 0)
		if err != nil {
			return 0, castErr("string", "int")
		}
		return int(n), nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "int")
		}
		return int(n), nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "int")
	}
}

// ToInt64E converts any to int64 and returns the result along with an error.
func ToInt64E(v any) (int64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint:
		// prevent a silent overflow to a negative value when the input exceeds math.MaxInt64
		if uint64(val) > math.MaxInt64 {
			return 0, castErr("uint", "int64")
		}
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		if val > math.MaxInt64 {
			return 0, castErr("uint64", "int64")
		}
		return int64(val), nil
	case float32:
		return floatToInt64E(float64(val), "float32")
	case float64:
		return floatToInt64E(val, "float64")
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0, castErr("string", "int64")
		}
		return n, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "int64")
		}
		return n, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "int64")
	}
}

// --- Unsigned integer conversion (uint) ---

// ToUint converts any to uint, returning the zero value if the conversion fails.
func ToUint(v any) uint {
	val, _ := ToUintE(v)
	return val
}

// ToUint64 converts any to uint64, returning the zero value if the conversion fails.
func ToUint64(v any) uint64 {
	val, _ := ToUint64E(v)
	return val
}

// ToUintE converts any to uint and returns the result along with an error.
func ToUintE(v any) (uint, error) {
	n, err := ToUint64E(v)
	if err != nil {
		return 0, err
	}
	// On 32-bit platforms uint is narrower than uint64, so overflow must be checked
	// (this is always false on 64-bit platforms).
	if n > math.MaxUint {
		return 0, castErr("uint64", "uint")
	}
	return uint(n), nil
}

// ToUint64E converts any to uint64 and returns the result along with an error.
func ToUint64E(v any) (uint64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		if val < 0 {
			return 0, castErr("int(negative)", "uint64")
		}
		return uint64(val), nil
	case int8:
		if val < 0 {
			return 0, castErr("int8(negative)", "uint64")
		}
		return uint64(val), nil
	case int16:
		if val < 0 {
			return 0, castErr("int16(negative)", "uint64")
		}
		return uint64(val), nil
	case int32:
		if val < 0 {
			return 0, castErr("int32(negative)", "uint64")
		}
		return uint64(val), nil
	case int64:
		if val < 0 {
			return 0, castErr("int64(negative)", "uint64")
		}
		return uint64(val), nil
	case uint:
		return uint64(val), nil
	case uint8:
		return uint64(val), nil
	case uint16:
		return uint64(val), nil
	case uint32:
		return uint64(val), nil
	case uint64:
		return val, nil
	case float32:
		return floatToUint64E(float64(val), "float32")
	case float64:
		return floatToUint64E(val, "float64")
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0, castErr("string", "uint64")
		}
		return n, nil
	case json.Number:
		n, err := strconv.ParseUint(val.String(), 10, 64)
		if err != nil {
			return 0, castErr("json.Number", "uint64")
		}
		return n, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "uint64")
	}
}

// --- Float conversion (float) ---

// ToFloat32 converts any to float32, returning the zero value if the conversion fails.
func ToFloat32(v any) float32 {
	val, _ := ToFloat32E(v)
	return val
}

// ToFloat64 converts any to float64, returning the zero value if the conversion fails.
func ToFloat64(v any) float64 {
	val, _ := ToFloat64E(v)
	return val
}

// ToFloat32E converts any to float32 and returns the result along with an error.
func ToFloat32E(v any) (float32, error) {
	f, err := ToFloat64E(v)
	if err != nil {
		return 0, err
	}
	f32 := float32(f)
	// A narrowing float64 → float32 overflow silently produces ±Inf (e.g. 1e300), so it
	// must be reported explicitly. An input that is already ±Inf is passed through and
	// is not treated as a narrowing overflow.
	if isNonFinite(float64(f32)) && !isNonFinite(f) {
		return 0, castErr("float64", "float32")
	}
	return f32, nil
}

// ToFloat64E converts any to float64 and returns the result along with an error.
func ToFloat64E(v any) (float64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return float64(val), nil
	case int8:
		return float64(val), nil
	case int16:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case uint:
		return float64(val), nil
	case uint8:
		return float64(val), nil
	case uint16:
		return float64(val), nil
	case uint32:
		return float64(val), nil
	case uint64:
		return float64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, castErr("string", "float64")
		}
		return f, nil
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, castErr("json.Number", "float64")
		}
		return f, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "float64")
	}
}
