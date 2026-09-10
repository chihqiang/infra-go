package mapping

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- lookupKeyCanonical：大小写不敏感键查找 ---

func TestLookupKeyCanonical(t *testing.T) {
	lower := strings.ToLower

	t.Run("exact match wins", func(t *testing.T) {
		v, ok, err := lookupKeyCanonical(map[string]any{"host": "a"}, "host", lower)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "a", v)
	})

	t.Run("exact match preferred over case variant", func(t *testing.T) {
		m := map[string]any{"host": "exact", "HOST": "variant"}
		v, ok, err := lookupKeyCanonical(m, "host", lower)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "exact", v, "exact match must win over case-insensitive fallback")
	})

	t.Run("canonical form matches", func(t *testing.T) {
		v, ok, err := lookupKeyCanonical(map[string]any{"logmode": "console"}, "logMode", lower)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "console", v)
	})

	t.Run("case variant matches", func(t *testing.T) {
		v, ok, err := lookupKeyCanonical(map[string]any{"LogMode": "console"}, "logMode", lower)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "console", v)
	})

	t.Run("chained keys", func(t *testing.T) {
		m := map[string]any{"DB": map[string]any{"HostName": "db"}}
		v, ok, err := lookupKeyCanonical(m, "db.hostname", lower)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "db", v)
	})

	t.Run("missing key", func(t *testing.T) {
		_, ok, err := lookupKeyCanonical(map[string]any{"other": 1}, "host", lower)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("nil map", func(t *testing.T) {
		_, ok, err := lookupKeyCanonical(nil, "host", lower)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("ambiguous case variants", func(t *testing.T) {
		m := map[string]any{"Host": 1, "HOST": 2}
		_, ok, err := lookupKeyCanonical(m, "host", lower)
		require.ErrorIs(t, err, errAmbiguousKey)
		assert.False(t, ok)
	})

	t.Run("ambiguous chained key reports error", func(t *testing.T) {
		m := map[string]any{"DB": map[string]any{"Host": 1, "HOST": 2}}
		_, _, err := lookupKeyCanonical(m, "db.host", lower)
		require.ErrorIs(t, err, errAmbiguousKey)
	})

	t.Run("non-map intermediate segment", func(t *testing.T) {
		_, ok, err := lookupKeyCanonical(map[string]any{"db": "not-a-map"}, "db.host", lower)
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// TestDescribeKeys_Sorted 验证错误信息中的键顺序稳定（便于测试与排障）。
func TestDescribeKeys_Sorted(t *testing.T) {
	assert.Equal(t, "B, a, c", describeKeys(map[string]any{"c": 1, "a": 2, "B": 3}))
}
