package mapping

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- utils 工具函数测试 ---

func TestDeref(t *testing.T) {
	assert.Equal(t, "int", Deref(reflect.TypeOf(int(1))).Kind().String())
	assert.Equal(t, "string", Deref(reflect.TypeOf((*string)(nil))).Kind().String())
	assert.Equal(t, "int", Deref(reflect.TypeOf((**int)(nil))).Kind().String())
}

func TestValidatePtr(t *testing.T) {
	var i int
	assert.NoError(t, ValidatePtr(reflect.ValueOf(&i)))
	assert.Error(t, ValidatePtr(reflect.ValueOf(i)))
	assert.Error(t, ValidatePtr(reflect.ValueOf((*int)(nil))))
}

func TestConvertTypeFromString(t *testing.T) {
	v, err := convertTypeFromString(reflect.Int, "42")
	assert.NoError(t, err)
	assert.Equal(t, int64(42), v)

	v, err = convertTypeFromString(reflect.Bool, "true")
	assert.NoError(t, err)
	assert.Equal(t, true, v)

	v, err = convertTypeFromString(reflect.Bool, "1")
	assert.NoError(t, err)
	assert.Equal(t, true, v)

	v, err = convertTypeFromString(reflect.Float64, "3.14")
	assert.NoError(t, err)
	assert.Equal(t, float64(3.14), v)

	_, err = convertTypeFromString(reflect.Int, "abc")
	assert.Error(t, err)
}

func TestConvertTypeFromString_MoreKinds(t *testing.T) {
	for _, kind := range []reflect.Kind{
		reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
	} {
		v, err := convertTypeFromString(kind, "1")
		assert.NoError(t, err, "kind %v", kind)
		assert.NotNil(t, v)
	}

	v, err := convertTypeFromString(reflect.Float32, "1.5")
	assert.NoError(t, err)
	assert.Equal(t, float64(1.5), v)

	v, err = convertTypeFromString(reflect.String, "x")
	assert.NoError(t, err)
	assert.Equal(t, "x", v)

	// 不支持的类型
	_, err = convertTypeFromString(reflect.Slice, "x")
	assert.Error(t, err)
}

func TestStructValueRequired(t *testing.T) {
	type RequiredStruct struct {
		Name string `json:"name"`
	}
	assert.True(t, structValueRequired("json", reflect.TypeOf(RequiredStruct{})))

	type OptionalStruct struct {
		Name string `json:",optional"`
	}
	assert.False(t, structValueRequired("json", reflect.TypeOf(OptionalStruct{})))

	type DefaultStruct struct {
		Name string `json:",default=hello"`
	}
	assert.False(t, structValueRequired("json", reflect.TypeOf(DefaultStruct{})))
}

func TestStructValueRequired_Nested(t *testing.T) {
	type Inner struct {
		Name string `json:"name"` // 必填
	}
	type Outer struct {
		Inner Inner `json:"inner"`
	}
	assert.True(t, structValueRequired("json", reflect.TypeOf(Outer{})))
}
