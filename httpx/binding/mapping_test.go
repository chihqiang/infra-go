package binding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 mapping.go：mapURI/mapForm/mapHeader/mapFormByTag、setter 与映射引擎。

// --- map 目标直接填充 ---

func TestMapForm_ToMapStringString(t *testing.T) {
	m := map[string]string{}
	err := mapForm(&m, map[string][]string{
		"a": {"1", "2"},
		"b": {"x"},
	})
	require.NoError(t, err)
	assert.Equal(t, "2", m["a"]) // 取最后一个值
	assert.Equal(t, "x", m["b"])
}

func TestMapForm_ToMapStringSlices(t *testing.T) {
	m := map[string][]string{}
	err := mapForm(&m, map[string][]string{
		"a": {"1", "2"},
		"b": {"x"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, m["a"])
	assert.Equal(t, []string{"x"}, m["b"])
}

func TestMapForm_ToBadMapTarget(t *testing.T) {
	m := map[string]int{}
	require.Error(t, mapForm(&m, map[string][]string{"a": {"1"}}))
}

// --- 结构体映射（匿名内嵌 / 指针字段自动分配 / 忽略） ---

type mappingInner struct {
	X string `form:"x"`
}

type mappingOuter struct {
	mappingInner             // 匿名内嵌，展开子字段
	Y            string      `form:"y"`
	P            *mappingPtr // 无 form 标签：命中子字段时自动分配指针
	Skip         string      `form:"-"`
}

type mappingPtr struct {
	Z string `form:"z"`
}

func TestMapForm_NestedAnonymousAndPtr(t *testing.T) {
	var o mappingOuter
	err := mapForm(&o, map[string][]string{
		"x": {"vx"},
		"y": {"vy"},
		"z": {"vz"},
	})
	require.NoError(t, err)
	assert.Equal(t, "vx", o.X)
	assert.Equal(t, "vy", o.Y)
	require.NotNil(t, o.P) // 指针字段在命中子字段时自动分配
	assert.Equal(t, "vz", o.P.Z)
	assert.Empty(t, o.Skip) // form:"-" 忽略
}

// --- header 映射走 headerSource ---

func TestMapHeader_CanonicalKey(t *testing.T) {
	type h struct {
		Token string `header:"x-token"` // 小写 tag → CanonicalMIMEHeaderKey 转大写后匹配
	}
	var v h
	// 数据源使用 canonical 键（http.Header 实际形态：X-Token）
	require.NoError(t, mapHeader(&v, map[string][]string{"X-Token": {"abc"}}))
	assert.Equal(t, "abc", v.Token)
}

// --- setByForm 的切片/默认值分支 ---

func TestSetByForm_SliceSplitAndDefault(t *testing.T) {
	// 单值含逗号自动拆分
	s := &struct {
		Tags []string
	}{}
	rv := reflectValueFieldOf(s, 0)
	ok, err := setByForm(rv, nil, map[string][]string{"tags": {"a,b,c"}}, "tags", setOptions{})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, s.Tags)

	// 缺省时取 default 值（逗号拆分为切片）
	ok, err = setByForm(rv, nil, map[string][]string{}, "tags", setOptions{isDefaultExists: true, defaultValue: "x,y"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"x", "y"}, s.Tags)

	// 无数据且无默认值 → 未设置
	ok, err = setByForm(rv, nil, map[string][]string{}, "tags", setOptions{})
	require.NoError(t, err)
	assert.False(t, ok)
}
