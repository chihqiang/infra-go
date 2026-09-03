package cast

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 泛型取址 Ptr 测试 ---

func TestPtr(t *testing.T) {
	p := Ptr("default")
	require.NotNil(t, p)
	assert.Equal(t, "default", *p)

	pn := Ptr(42)
	require.NotNil(t, pn)
	assert.Equal(t, 42, *pn)
}

// --- 安全解引用 Val 测试 ---

func TestVal(t *testing.T) {
	// 非 nil：返回指针指向的值
	p := Ptr(42)
	assert.Equal(t, 42, Val(p))

	// nil + 无默认值：返回零值
	assert.Equal(t, 0, Val[int](nil))

	// nil + 提供默认值：返回默认值
	assert.Equal(t, 7, Val[int](nil, 7))

	// 字符串场景
	assert.Equal(t, "", Val[string](nil))
	assert.Equal(t, "fb", Val[string](nil, "fb"))
	s := Ptr("ok")
	assert.Equal(t, "ok", Val(s))
}

// --- ToXxxPtr 系列成功/失败测试 ---

func TestToXxxPtr_Success(t *testing.T) {
	assert.Equal(t, 123, *ToIntPtr("123"))
	assert.Equal(t, int64(42), *ToInt64Ptr("42"))
	assert.Equal(t, uint(42), *ToUintPtr("42"))
	assert.Equal(t, uint64(42), *ToUint64Ptr("42"))
	assert.Equal(t, float32(3.14), *ToFloat32Ptr("3.14"))
	assert.Equal(t, float64(3.14), *ToFloat64Ptr("3.14"))
	assert.Equal(t, "456", *ToStringPtr(456))
	assert.Equal(t, true, *ToBoolPtr("true"))
	assert.Equal(t, 5*time.Second, *ToDurationPtr("5s"))

	tm := ToTimePtr("2024-01-15T10:30:00Z")
	require.NotNil(t, tm)
	assert.Equal(t, 2024, tm.Year())
}

func TestToXxxPtr_Failure(t *testing.T) {
	// 转换失败一律返回 nil，便于直接用于可选指针字段
	assert.Nil(t, ToIntPtr("abc"))
	assert.Nil(t, ToInt64Ptr("abc"))
	assert.Nil(t, ToUintPtr(-1))
	assert.Nil(t, ToUint64Ptr(-1))
	assert.Nil(t, ToFloat32Ptr("abc"))
	assert.Nil(t, ToFloat64Ptr("abc"))
	assert.Nil(t, ToBoolPtr("not a bool"))
	assert.Nil(t, ToDurationPtr("not a duration"))
	assert.Nil(t, ToTimePtr("not a time"))
	// ToString 极少失败，但 unsupported 结构体可触发失败
	assert.Nil(t, ToStringPtr(func() {}))
}
