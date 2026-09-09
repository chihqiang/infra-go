package conf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- nil / 空值边界 ---

func TestLowercaseKeys_Nil(t *testing.T) {
	assert.Nil(t, lowercaseKeys(nil))
}

func TestLowercaseValues_Nil(t *testing.T) {
	assert.Nil(t, lowercaseValues(nil))
}

func TestNormalizeMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeMap(nil))
}

// --- normalizeValue 各类型 ---

func TestNormalizeValue_NumericTypes(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int(1), "1"},
		{int8(8), "8"},
		{int16(16), "16"},
		{int32(32), "32"},
		{int64(64), "64"},
		{uint(7), "7"},
		{uint8(1), "1"},
		{uint16(2), "2"},
		{uint32(3), "3"},
		{uint64(4), "4"},
		{float32(1.5), "1.5"},
		{float64(2.5), "2.5"},
		{json.Number("9"), "9"},
	}
	for _, c := range cases {
		got := normalizeValue(c.in)
		assert.Equal(t, json.Number(c.want), got, "normalizeValue(%#v)", c.in)
	}
}

func TestNormalizeValue_Scalar(t *testing.T) {
	// bool / string 原样返回
	assert.Equal(t, true, normalizeValue(true))
	assert.Equal(t, "str", normalizeValue("str"))
	// nil 原样返回
	assert.Nil(t, normalizeValue(nil))
	// 默认分支：无法识别的类型 → fmt.Sprintf("%v")
	assert.Equal(t, "{y}", normalizeValue(struct{ x string }{"y"}))
}

func TestNormalizeValue_Maps(t *testing.T) {
	// map[string]any → normalizeMap
	got := normalizeValue(map[string]any{"k": int(1)})
	assert.Equal(t, map[string]any{"k": json.Number("1")}, got)

	// map[any]any → normalizeAnyKeyMap
	got = normalizeValue(map[any]any{1: "a"})
	assert.Equal(t, map[string]any{"1": "a"}, got)
}

func TestNormalizeValue_Slices(t *testing.T) {
	// []any → normalizeSlice
	got := normalizeValue([]any{int(1), "a", true})
	assert.Equal(t, []any{json.Number("1"), "a", true}, got)

	// []map[string]any
	got = normalizeValue([]map[string]any{{"k": int(1)}})
	assert.Equal(t, []any{map[string]any{"k": json.Number("1")}}, got)

	// []map[any]any
	got = normalizeValue([]map[any]any{{int(1): "a"}})
	assert.Equal(t, []any{map[string]any{"1": "a"}}, got)
}

func TestNormalizeAnyKeyMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeAnyKeyMap(nil))
}

func TestNormalizeSlice_Nil(t *testing.T) {
	assert.Nil(t, normalizeSlice(nil))
}

// --- unmarshalMap：大小写不敏感反序列化 ---

func TestUnmarshalMap_LowercasesKeys(t *testing.T) {
	var cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	err := unmarshalMap(map[string]any{"HOST": "x", "PORT": json.Number("1")}, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Host)
	assert.Equal(t, 1, cfg.Port)
}
