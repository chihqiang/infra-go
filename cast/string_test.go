package cast

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	// error 类型实现 fmt.Stringer，应转为错误消息
	assert.Equal(t, "custom", ToString(errors.New("custom")))
}

// --- 补充：ToStringE 类型族 ---

func TestToStringE_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "s", "s"},
		{"int", 42, "42"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"uint", uint(42), "42"},
		{"uint8", uint8(1), "1"},
		{"uint16", uint16(2), "2"},
		{"uint32", uint32(3), "3"},
		{"uint64", uint64(4), "4"},
		{"float32", float32(1.5), "1.5"},
		{"float64", 2.5, "2.5"},
		{"bool", true, "true"},
		{"json.Number", json.Number("42"), "42"},
		{"bytes", []byte("hi"), "hi"},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToStringE(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToStringE_StringerType(t *testing.T) {
	// 非 error 的 fmt.Stringer（time.Time）走 Stringer 分支
	s, err := ToStringE(time.Unix(1700000000, 0).UTC())
	require.NoError(t, err)
	assert.NotEmpty(t, s)
}

func TestToStringE_DefaultJSONSuccess(t *testing.T) {
	// default 分支：json.Marshal 成功的切片 → "[1,2]"
	s, err := ToStringE([]int{1, 2})
	require.NoError(t, err)
	assert.Equal(t, "[1,2]", s)

	// map 序列化
	s, err = ToStringE(map[string]int{"a": 1})
	require.NoError(t, err)
	assert.Contains(t, s, `"a":1`)
}

func TestToStringE_Errors(t *testing.T) {
	// func 无法序列化
	_, err := ToStringE(func() {})
	assert.Error(t, err)
}
