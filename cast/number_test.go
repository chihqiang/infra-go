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
