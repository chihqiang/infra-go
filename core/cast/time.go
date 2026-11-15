package cast

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// --- Time type conversion ---

// ToDuration converts any to time.Duration, returning the zero value if the conversion fails.
// Supports numeric types (nanoseconds) and strings (such as "5s" or "100ms").
func ToDuration(v any) time.Duration {
	val, _ := ToDurationE(v)
	return val
}

// ToDurationE converts any to time.Duration and returns the result along with an error.
// Numeric types are treated as nanoseconds; strings are parsed with time.ParseDuration.
func ToDurationE(v any) (time.Duration, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case time.Duration:
		return val, nil
	case int:
		return time.Duration(val), nil
	case int8:
		return time.Duration(val), nil
	case int16:
		return time.Duration(val), nil
	case int32:
		return time.Duration(val), nil
	case int64:
		return time.Duration(val), nil
	case uint:
		return time.Duration(val), nil
	case uint8:
		return time.Duration(val), nil
	case uint16:
		return time.Duration(val), nil
	case uint32:
		return time.Duration(val), nil
	case uint64:
		return time.Duration(val), nil
	case float32:
		return time.Duration(val), nil
	case float64:
		return time.Duration(val), nil
	case string:
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return 0, castErr("string", "time.Duration")
		}
		return d, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "time.Duration")
		}
		return time.Duration(n), nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "time.Duration")
	}
}

// --- Time conversion (time.Time) ---

// ToTime converts any to time.Time, returning the zero value if the conversion fails.
// Supports strings (RFC3339 or a Unix timestamp) and numeric types (Unix timestamp).
func ToTime(v any) time.Time {
	val, _ := ToTimeE(v)
	return val
}

// ToTimeE converts any to time.Time and returns the result along with an error.
// Strings are parsed as RFC3339 first and then as a Unix timestamp.
// Numeric types are treated as Unix timestamps (seconds).
func ToTimeE(v any) (time.Time, error) {
	switch val := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return val, nil
	case string:
		s := strings.TrimSpace(val)
		// try RFC3339
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			return t, nil
		}
		// try a Unix timestamp
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0), nil
		}
		return time.Time{}, castErr("string", "time.Time")
	case int:
		return time.Unix(int64(val), 0), nil
	case int8:
		return time.Unix(int64(val), 0), nil
	case int16:
		return time.Unix(int64(val), 0), nil
	case int32:
		return time.Unix(int64(val), 0), nil
	case int64:
		return time.Unix(val, 0), nil
	case uint:
		return time.Unix(int64(val), 0), nil
	case uint8:
		return time.Unix(int64(val), 0), nil
	case uint16:
		return time.Unix(int64(val), 0), nil
	case uint32:
		return time.Unix(int64(val), 0), nil
	case uint64:
		return time.Unix(int64(val), 0), nil
	case float32:
		return time.Unix(int64(val), 0), nil
	case float64:
		return time.Unix(int64(val), 0), nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return time.Time{}, castErr("json.Number", "time.Time")
		}
		return time.Unix(n, 0), nil
	default:
		return time.Time{}, castErr(reflect.TypeOf(v).String(), "time.Time")
	}
}
