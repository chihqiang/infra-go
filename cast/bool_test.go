package cast

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// --- 补充：ToBoolE 类型族与失败分支 ---

func TestToBoolE_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  bool
	}{
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"int", 0, false},
		{"int8", int8(1), true},
		{"int16", int16(0), false},
		{"int32", int32(1), true},
		{"int64", int64(0), false},
		{"uint", uint(1), true},
		{"uint8", uint8(0), false},
		{"uint16", uint16(1), true},
		{"uint32", uint32(0), false},
		{"uint64", uint64(1), true},
		{"float32", float32(0), false},
		{"float64", 1.5, true},
		{"json.Number_1", json.Number("1"), true},
		{"json.Number_0", json.Number("0"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToBoolE(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToBoolE_Errors(t *testing.T) {
	// json.Number 无法转 int64（含小数）→ 错误
	_, err := ToBoolE(json.Number("1.5"))
	assert.Error(t, err)
	_, err = ToBoolE(json.Number("abc"))
	assert.Error(t, err)
	// 不支持的类型
	_, err = ToBoolE([]int{1})
	assert.Error(t, err)
}
