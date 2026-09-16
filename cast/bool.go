package cast

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// --- Boolean conversion ---

// ToBool converts any to bool, returning false if the conversion fails.
// Supports bool, string ("true"/"1") and int (non-zero is true).
func ToBool(v any) bool {
	val, _ := ToBoolE(v)
	return val
}

// ToBoolE converts any to bool and returns the result along with an error.
// Supported strings: 1/t/T/true/TRUE/True → true, 0/f/F/false/FALSE/False → false.
func ToBoolE(v any) (bool, error) {
	switch val := v.(type) {
	case nil:
		return false, nil
	case bool:
		return val, nil
	case int:
		return val != 0, nil
	case int8:
		return val != 0, nil
	case int16:
		return val != 0, nil
	case int32:
		return val != 0, nil
	case int64:
		return val != 0, nil
	case uint:
		return val != 0, nil
	case uint8:
		return val != 0, nil
	case uint16:
		return val != 0, nil
	case uint32:
		return val != 0, nil
	case uint64:
		return val != 0, nil
	case float32:
		return val != 0, nil
	case float64:
		return val != 0, nil
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err != nil {
			return false, castErr("string", "bool")
		}
		return b, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return false, castErr("json.Number", "bool")
		}
		return n != 0, nil
	default:
		return false, castErr(reflect.TypeOf(v).String(), "bool")
	}
}
