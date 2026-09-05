package cast

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToInt 测试 ---

func TestToInt(t *testing.T) {
	tests := []struct {
		name  string
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
		{"string_zero_padded", "08", 8},
		{"string_hex_not_supported", "0xff", 0}, // 十六进制不再被意外接受，统一按十进制解析
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

func TestToIntE_NonFiniteFloat(t *testing.T) {
	// NaN 与 Inf 无法安全转换为整数，应返回错误而非未定义值
	_, err := ToIntE(math.NaN())
	assert.Error(t, err)
	_, err = ToIntE(math.Inf(1))
	assert.Error(t, err)

	_, err = ToInt64E(math.NaN())
	assert.Error(t, err)
	_, err = ToInt64E(math.Inf(-1))
	assert.Error(t, err)

	_, err = ToUint64E(math.NaN())
	assert.Error(t, err)
	_, err = ToUint64E(math.Inf(1))
	assert.Error(t, err)
}

func TestToIntE_Overflow(t *testing.T) {
	// uint64 大于 int 最大值时应返回错误，而非静默溢出为负值
	_, err := ToIntE(uint64(math.MaxUint64))
	assert.Error(t, err)

	_, err = ToInt64E(uint64(math.MaxUint64))
	assert.Error(t, err)

	// 边界内的值正常转换
	n, err := ToInt64E(uint64(math.MaxInt64))
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), n)
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

// --- 补充：E 系列全类型族与失败分支 ---

func TestToInt64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int64
	}{
		{"int", int(1), 1},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(5), 5},
		{"uint8", uint8(1), 1},
		{"uint16", uint16(2), 2},
		{"uint32", uint32(3), 3},
		{"uint64", uint64(4), 4},
		{"float32", float32(3), 3},
		{"float64", 3.14, 3},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"json.Number", json.Number("9"), 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToInt64E(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToInt64E_Errors(t *testing.T) {
	_, err := ToInt64E("abc")
	assert.Error(t, err)
	_, err = ToInt64E(json.Number("1e2")) // Int64() 无法解析
	assert.Error(t, err)
	_, err = ToInt64E([]int{1})
	assert.Error(t, err)
	_, err = ToInt64E(uint(math.MaxUint64)) // 溢出
	assert.Error(t, err)
}

func TestToUint64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  uint64
	}{
		{"int", int(42), 42},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(7), 7},
		{"uint8", uint8(1), 1},
		{"uint16", uint16(2), 2},
		{"uint32", uint32(3), 3},
		{"uint64", uint64(4), 4},
		{"float32", float32(3.5), 3},
		{"float64", 3.5, 3},
		{"bool_true", true, 1},
		{"json.Number", json.Number("7"), 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToUint64E(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToUint64E_Errors(t *testing.T) {
	_, err := ToUint64E(nil)
	assert.NoError(t, err) // nil → 0, 无错误

	_, err = ToUint64E(int8(-1))
	assert.Error(t, err)
	_, err = ToUint64E(int64(-5))
	assert.Error(t, err)
	_, err = ToUint64E(float64(-1))
	assert.Error(t, err)
	_, err = ToUint64E("abc")
	assert.Error(t, err)
	_, err = ToUint64E(json.Number("1e2"))
	assert.Error(t, err)
	_, err = ToUint64E([]int{1})
	assert.Error(t, err)
}

func TestToUint64(t *testing.T) {
	assert.Equal(t, uint64(42), ToUint64(uint64(42)))
	assert.Equal(t, uint64(0), ToUint64("-1")) // 失败返回零值
	assert.Equal(t, uint64(42), ToUint64("42"))
}

func TestToFloat64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  float64
	}{
		{"int", int(3), 3},
		{"int8", int8(3), 3},
		{"int64", int64(3), 3},
		{"uint", uint(3), 3},
		{"uint64", uint64(3), 3},
		{"float32", float32(1.5), 1.5},
		{"float64", 2.5, 2.5},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"json.Number", json.Number("3"), 3},
		{"string", "3.14", 3.14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToFloat64E(c.input)
			require.NoError(t, err)
			assert.InDelta(t, c.want, got, 0.001)
		})
	}
}

func TestToFloat64E_Errors(t *testing.T) {
	_, err := ToFloat64E(nil)
	assert.NoError(t, err) // nil → 0

	_, err = ToFloat64E(json.Number("abc"))
	assert.Error(t, err)
	_, err = ToFloat64E([]int{1})
	assert.Error(t, err)
}

func TestToIntE_ExtraEdges(t *testing.T) {
	// uint 溢出（32 位平台边界以 math.MaxInt 判定）
	_, err := ToIntE(uint(math.MaxUint64))
	assert.Error(t, err)

	// float32 非有限值
	_, err = ToIntE(float32(math.Inf(1)))
	assert.Error(t, err)

	// json.Number Int64 失败
	_, err = ToIntE(json.Number("1.5"))
	assert.Error(t, err)

	// 负数 json.Number 正常
	n, err := ToIntE(json.Number("-7"))
	require.NoError(t, err)
	assert.Equal(t, -7, n)
}
