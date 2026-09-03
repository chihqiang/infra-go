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
