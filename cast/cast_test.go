package cast

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToInt 测试 ---

func TestToInt(t *testing.T) {
	tests := []struct {
		name string
		input any
		want  int
	}{
		{"int", 42, 42},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(42), 42},
		{"uint8", uint8(8), 8},
		{"uint16", uint16(16), 16},
		{"uint32", uint32(32), 32},
		{"uint64", uint64(64), 64},
		{"float32", float32(3.14), 3},
		{"float64", float64(3.99), 3},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"string", "123", 123},
		{"string_hex", "0xff", 255},
		{"json.Number", json.Number("42"), 42},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToInt(tt.input))
		})
	}
}

func TestToIntE_Error(t *testing.T) {
	_, err := ToIntE("abc")
	assert.Error(t, err)

	_, err = ToIntE([]int{1, 2})
	assert.Error(t, err)
}

// --- ToInt64 测试 ---

func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(42), ToInt64(42))
	assert.Equal(t, int64(42), ToInt64("42"))
	assert.Equal(t, int64(42), ToInt64(json.Number("42")))
	assert.Equal(t, int64(0), ToInt64(nil))
}

func TestToInt64E_LargeNumber(t *testing.T) {
	n, err := ToInt64E("9223372036854775807")
	require.NoError(t, err)
	assert.Equal(t, int64(9223372036854775807), n)
}

// --- ToUint 测试 ---

func TestToUint(t *testing.T) {
	assert.Equal(t, uint(42), ToUint(uint(42)))
	assert.Equal(t, uint(42), ToUint("42"))
	assert.Equal(t, uint(42), ToUint(int64(42)))
}

func TestToUint64E_Negative(t *testing.T) {
	_, err := ToUint64E(-1)
	assert.Error(t, err)
}

// --- ToFloat 测试 ---

func TestToFloat64(t *testing.T) {
	assert.Equal(t, float64(3.14), ToFloat64(3.14))
	assert.Equal(t, float64(3.14), ToFloat64("3.14"))
	assert.Equal(t, float64(3), ToFloat64(3))
	assert.Equal(t, float64(3), ToFloat64(json.Number("3")))
}

func TestToFloat32(t *testing.T) {
	assert.Equal(t, float32(3.14), ToFloat32("3.14"))
}

func TestToFloat64E_Error(t *testing.T) {
	_, err := ToFloat64E("not a number")
	assert.Error(t, err)
}

// --- ToString 测试 ---

func TestToString(t *testing.T) {
	assert.Equal(t, "hello", ToString("hello"))
	assert.Equal(t, "42", ToString(42))
	assert.Equal(t, "42", ToString(int64(42)))
	assert.Equal(t, "42", ToString(uint(42)))
	assert.Equal(t, "3.14", ToString(3.14))
	assert.Equal(t, "true", ToString(true))
	assert.Equal(t, "hello", ToString([]byte("hello")))
	assert.Equal(t, "42", ToString(json.Number("42")))
	assert.Equal(t, "", ToString(nil))
}

func TestToString_Stringer(t *testing.T) {
	type myStringer struct{}
	// myStringer 实现 fmt.Stringer
	assert.Equal(t, "custom", ToString(errors.New("custom")))
}

// --- ToBool 测试 ---

func TestToBool(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  bool
	}{
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"int_1", 1, true},
		{"int_0", 0, false},
		{"string_true", "true", true},
		{"string_1", "1", true},
		{"string_false", "false", false},
		{"string_0", "0", false},
		{"string_T", "T", true},
		{"string_F", "F", false},
		{"float_1", 1.0, true},
		{"float_0", 0.0, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToBool(tt.input))
		})
	}
}

func TestToBoolE_Error(t *testing.T) {
	_, err := ToBoolE("not a bool")
	assert.Error(t, err)
}

// --- ToDuration 测试 ---

func TestToDuration(t *testing.T) {
	assert.Equal(t, 5*time.Second, ToDuration("5s"))
	assert.Equal(t, 100*time.Millisecond, ToDuration("100ms"))
	assert.Equal(t, time.Duration(42), ToDuration(42))
	assert.Equal(t, time.Duration(0), ToDuration(nil))
}

func TestToDurationE_Error(t *testing.T) {
	_, err := ToDurationE("not a duration")
	assert.Error(t, err)
}

// --- ToTime 测试 ---

func TestToTime(t *testing.T) {
	// RFC3339
	tm := ToTime("2024-01-15T10:30:00Z")
	assert.Equal(t, 2024, tm.Year())
	assert.Equal(t, time.January, tm.Month())
	assert.Equal(t, 15, tm.Day())

	// Unix 时间戳
	tm2 := ToTime("1700000000")
	assert.Equal(t, 2023, tm2.Year())

	// 数值
	tm3 := ToTime(int64(1700000000))
	assert.Equal(t, 2023, tm3.Year())

	// nil
	assert.True(t, ToTime(nil).IsZero())
}

func TestToTimeE_Error(t *testing.T) {
	_, err := ToTimeE("not a time")
	assert.Error(t, err)
}

// --- ToIntSlice 测试 ---

func TestToIntSlice(t *testing.T) {
	// []int
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]int{1, 2, 3}))

	// []any
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]any{1, 2, 3}))

	// []string
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]string{"1", "2", "3"}))

	// 逗号分隔字符串
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice("1,2,3"))

	// 空字符串
	assert.Equal(t, []int{}, ToIntSlice(""))

	// nil
	assert.Equal(t, []int{}, ToIntSlice(nil))
}

func TestToIntSliceE_Error(t *testing.T) {
	_, err := ToIntSliceE([]string{"1", "abc"})
	assert.Error(t, err)
}

// --- ToStringSlice 测试 ---

func TestToStringSlice(t *testing.T) {
	// []string
	assert.Equal(t, []string{"a", "b"}, ToStringSlice([]string{"a", "b"}))

	// []any
	assert.Equal(t, []string{"1", "2"}, ToStringSlice([]any{1, 2}))

	// 逗号分隔字符串
	assert.Equal(t, []string{"a", "b", "c"}, ToStringSlice("a,b,c"))

	// []byte
	assert.Equal(t, []string{"hello"}, ToStringSlice([]byte("hello")))

	// 空字符串
	assert.Equal(t, []string{}, ToStringSlice(""))

	// nil
	assert.Equal(t, []string{}, ToStringSlice(nil))
}

// --- 泛型 To 测试 ---

func TestTo_Generic(t *testing.T) {
	assert.Equal(t, 123, To[int]("123"))
	assert.Equal(t, int64(123), To[int64]("123"))
	assert.Equal(t, uint(42), To[uint]("42"))
	assert.Equal(t, "456", To[string](456))
	assert.Equal(t, true, To[bool]("true"))
	assert.Equal(t, 3.14, To[float64]("3.14"))
	assert.Equal(t, 5*time.Second, To[time.Duration]("5s"))
}

func TestTo_GenericStruct(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	u := To[User](map[string]any{"name": "Alice", "age": 30})
	assert.Equal(t, "Alice", u.Name)
	assert.Equal(t, 30, u.Age)
}

// --- ErrCastFailed 测试 ---

func TestErrCastFailed(t *testing.T) {
	_, err := ToIntE("abc")
	assert.Error(t, err)

	var castErr *ErrCastFailed
	assert.True(t, errors.As(err, &castErr))
	assert.Equal(t, "string", castErr.From)
	assert.Equal(t, "int", castErr.To)
	assert.Contains(t, err.Error(), "cast")
	assert.Contains(t, err.Error(), "string")
	assert.Contains(t, err.Error(), "int")
}
