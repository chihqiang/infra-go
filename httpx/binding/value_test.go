package binding

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 value.go：setWithProperType 及各 set*Field / setFormMap。

// reflectValueFieldOf 返回结构体指针第 idx 个字段的可设置 reflect.Value。
func reflectValueFieldOf(ptr any, idx int) reflect.Value {
	return reflect.ValueOf(ptr).Elem().Field(idx)
}

func TestSetIntField_Overflow(t *testing.T) {
	var v int8
	require.Error(t, setIntField("300", 8, reflect.ValueOf(&v).Elem())) // int8 溢出
	assert.Zero(t, v)

	require.NoError(t, setIntField("42", 8, reflect.ValueOf(&v).Elem()))
	assert.Equal(t, int8(42), v)
}

func TestSetUintField_Overflow(t *testing.T) {
	var v uint8
	require.Error(t, setUintField("-1", 8, reflect.ValueOf(&v).Elem()))
}

func TestSetBoolField(t *testing.T) {
	var v bool
	require.NoError(t, setBoolField("true", reflect.ValueOf(&v).Elem()))
	assert.True(t, v)
	require.NoError(t, setBoolField("", reflect.ValueOf(&v).Elem()))
	assert.False(t, v)
	require.Error(t, setBoolField("notabool", reflect.ValueOf(&v).Elem()))
}

func TestSetFloatField_Overflow(t *testing.T) {
	var v float32
	require.Error(t, setFloatField("1e1000", 32, reflect.ValueOf(&v).Elem()))
}

func TestSetTimeField_Unix(t *testing.T) {
	var tm time.Time
	fm := &fieldMeta{timeFormat: "unix"}
	require.NoError(t, setTimeField("1700000000", fm, reflect.ValueOf(&tm).Elem()))
	assert.Equal(t, int64(1700000000), tm.Unix())
}

func TestSetTimeField_LayoutAndUTC(t *testing.T) {
	var tm time.Time
	fm := &fieldMeta{timeFormat: "2006-01-02", timeUTC: true}
	require.NoError(t, setTimeField("2024-05-06", fm, reflect.ValueOf(&tm).Elem()))
	assert.Equal(t, time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC), tm)

	// 空值 → 零值
	require.NoError(t, setTimeField("", fm, reflect.ValueOf(&tm).Elem()))
	assert.True(t, tm.IsZero())
}

func TestSetTimeDuration(t *testing.T) {
	var d time.Duration
	require.NoError(t, setTimeDuration("1m30s", reflect.ValueOf(&d).Elem()))
	assert.Equal(t, 90*time.Second, d)

	require.NoError(t, setTimeDuration("", reflect.ValueOf(&d).Elem()))
	assert.Zero(t, d)
}

func TestSetWithProperType_Unknown(t *testing.T) {
	var v complex128
	err := setWithProperType("1", reflect.ValueOf(&v).Elem(), nil, setOptions{})
	assert.ErrorIs(t, err, errUnknownType)
}

func TestSetWithProperType_StructJSONFallback(t *testing.T) {
	type sub struct {
		A int `json:"a"`
	}
	type wrap struct {
		S sub `form:"s"`
	}
	var w wrap
	fm := &fieldMeta{}
	require.NoError(t, setWithProperType(`{"a":5}`, reflect.ValueOf(&w).Elem().Field(0), fm, setOptions{}))
	assert.Equal(t, 5, w.S.A)
}

func TestSetFormMap_StringString(t *testing.T) {
	m := map[string]string{}
	// setFormMap 期望 map 值（mapFormByTag 会先解引用指针再传入）
	require.NoError(t, setFormMap(m, map[string][]string{"k": {"a", "b"}}))
	assert.Equal(t, "b", m["k"]) // 取最后值
}

func TestSetFormMap_StringSlices(t *testing.T) {
	m := map[string][]string{}
	require.NoError(t, setFormMap(m, map[string][]string{"k": {"a"}, "j": {"x", "y"}}))
	assert.Equal(t, []string{"a"}, m["k"])
	assert.Equal(t, []string{"x", "y"}, m["j"])
}

func TestSetFormMap_WrongTarget(t *testing.T) {
	m := map[string]int{}
	require.Error(t, setFormMap(m, map[string][]string{"k": {"a"}}))
}
