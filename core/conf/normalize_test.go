package conf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- nil / empty value edge cases ---

func TestNormalizeMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeMap(nil))
}

// --- normalizeValue by type ---

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
	// bool / string are returned as is
	assert.Equal(t, true, normalizeValue(true))
	assert.Equal(t, "str", normalizeValue("str"))
	// nil is returned as is
	assert.Nil(t, normalizeValue(nil))
	// default branch: unrecognised type -> fmt.Sprintf("%v")
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

// --- unmarshalMap: case-insensitive unmarshalling ---

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

// TestUnmarshalMap_CaseInsensitiveFieldName verifies that case-insensitive field name
// matching does not rely on rewriting the keys of the input map up front.
func TestUnmarshalMap_CaseInsensitiveFieldName(t *testing.T) {
	var cfg struct {
		LogMode string `json:"LogMode"`
		Nested  struct {
			HostName string `json:"HostName"`
		} `json:"Nested"`
	}
	err := unmarshalMap(map[string]any{
		"logmode": "console",
		"nested":  map[string]any{"hostname": "db"},
	}, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "console", cfg.LogMode)
	assert.Equal(t, "db", cfg.Nested.HostName)
}

// TestUnmarshalMap_MapFieldKeysPreserved verifies that the keys of map fields are kept as
// is. Historical defect: unmarshalMap recursively lowercased the whole config tree, which
// rewrote the data keys of map fields as well.
func TestUnmarshalMap_MapFieldKeysPreserved(t *testing.T) {
	var cfg struct {
		Labels map[string]string `json:"labels"`
		Counts map[string]int    `json:"counts"`
	}
	err := unmarshalMap(map[string]any{
		"labels": map[string]any{"AppName": "svc", "Env": "prod"},
		"counts": map[string]any{"RetryCount": json.Number("3")},
	}, &cfg)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"AppName": "svc", "Env": "prod"}, cfg.Labels)
	assert.Equal(t, map[string]int{"RetryCount": 3}, cfg.Counts)
}

// TestUnmarshalMap_MapFieldKeysNotMerged verifies that keys differing only in case are not
// merged. Historical defect: after lowercasing, AppName and appname landed in the same key
// and the outcome depended on map iteration order.
func TestUnmarshalMap_MapFieldKeysNotMerged(t *testing.T) {
	var cfg struct {
		Labels map[string]string `json:"labels"`
	}
	err := unmarshalMap(map[string]any{
		"labels": map[string]any{"AppName": "upper", "appname": "lower"},
	}, &cfg)
	require.NoError(t, err)

	require.Len(t, cfg.Labels, 2, "distinct keys must not be merged")
	assert.Equal(t, "upper", cfg.Labels["AppName"])
	assert.Equal(t, "lower", cfg.Labels["appname"])
}

// TestUnmarshalMap_NestedMapInSlicePreservesKeys verifies that the keys of maps inside
// slices are kept as is too.
func TestUnmarshalMap_NestedMapInSlicePreservesKeys(t *testing.T) {
	var cfg struct {
		Items []map[string]string `json:"items"`
	}
	err := unmarshalMap(map[string]any{
		"items": []any{
			map[string]any{"KeyName": "a"},
			map[string]any{"OtherKey": "b"},
		},
	}, &cfg)
	require.NoError(t, err)

	require.Len(t, cfg.Items, 2)
	assert.Equal(t, map[string]string{"KeyName": "a"}, cfg.Items[0])
	assert.Equal(t, map[string]string{"OtherKey": "b"}, cfg.Items[1])
}
