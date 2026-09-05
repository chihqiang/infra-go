package cast

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ToIntSlice 测试 ---

func TestToIntSlice(t *testing.T) {
	// []int
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]int{1, 2, 3}))

	// []any
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]any{1, 2, 3}))

	// []string
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]string{"1", "2", "3"}))

	// 逗号分隔字符串
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice("1,2,3"))

	// 空字符串
	assert.Equal(t, []int{}, ToIntSlice(""))

	// nil
	assert.Equal(t, []int{}, ToIntSlice(nil))
}

func TestToIntSliceE_Error(t *testing.T) {
	_, err := ToIntSliceE([]string{"1", "abc"})
	assert.Error(t, err)
}

func TestToIntSliceE_MoreErrors(t *testing.T) {
	// []any 中含不可转元素
	_, err := ToIntSliceE([]any{1, "abc"})
	assert.Error(t, err)

	// 逗号字符串中含非法数字段
	_, err = ToIntSliceE("1,abc")
	assert.Error(t, err)

	// 不支持的类型
	_, err = ToIntSliceE(map[string]int{})
	assert.Error(t, err)
}

// --- ToStringSlice 测试 ---

func TestToStringSlice(t *testing.T) {
	// []string
	assert.Equal(t, []string{"a", "b"}, ToStringSlice([]string{"a", "b"}))

	// []any
	assert.Equal(t, []string{"1", "2"}, ToStringSlice([]any{1, 2}))

	// 逗号分隔字符串
	assert.Equal(t, []string{"a", "b", "c"}, ToStringSlice("a,b,c"))

	// []byte
	assert.Equal(t, []string{"hello"}, ToStringSlice([]byte("hello")))

	// 空字符串
	assert.Equal(t, []string{}, ToStringSlice(""))

	// nil
	assert.Equal(t, []string{}, ToStringSlice(nil))
}

func TestToStringSliceE_Errors(t *testing.T) {
	// []any 中含不可字符串化元素（func 序列化失败）
	_, err := ToStringSliceE([]any{func() {}})
	assert.Error(t, err)

	// 不支持的类型
	_, err = ToStringSliceE(map[string]string{})
	assert.Error(t, err)
}
