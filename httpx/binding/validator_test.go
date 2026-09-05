package binding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 validator.go：DefaultValidator、SetValidateFn/validate/Validate。

type validReq struct {
	Name  string `binding:"required"`
	Email string `binding:"required,email"`
}

func TestDefaultValidator_StructValid(t *testing.T) {
	v := &DefaultValidator{}
	require.NoError(t, v.ValidateStruct(&validReq{Name: "Alice", Email: "a@b.com"}))
}

func TestDefaultValidator_StructInvalid(t *testing.T) {
	v := &DefaultValidator{}
	require.Error(t, v.ValidateStruct(&validReq{Name: "Alice"})) // 缺 email
}

func TestDefaultValidator_ValueNotPointer(t *testing.T) {
	v := &DefaultValidator{}
	require.NoError(t, v.ValidateStruct(validReq{Name: "Alice", Email: "a@b.com"}))
}

func TestDefaultValidator_SliceAndArray(t *testing.T) {
	v := &DefaultValidator{}
	// 全部合法 → nil
	require.NoError(t, v.ValidateStruct([]validReq{
		{Name: "a", Email: "a@b.com"},
		{Name: "b", Email: "c@d.com"},
	}))
	// 含非法元素 → 报错
	require.Error(t, v.ValidateStruct([]validReq{
		{Name: "a", Email: "a@b.com"},
		{Name: "b"},
	}))
	// 数组同理
	require.Error(t, v.ValidateStruct([1]validReq{{Name: "b"}}))
}

func TestDefaultValidator_NilAndScalar(t *testing.T) {
	v := &DefaultValidator{}
	require.NoError(t, v.ValidateStruct(nil))
	require.NoError(t, v.ValidateStruct(42))           // 非 struct 直接跳过
	require.NoError(t, v.ValidateStruct("plain text")) // Ptr→非 struct → 继续解引用到 string
}

func TestDefaultValidator_Engine(t *testing.T) {
	v := &DefaultValidator{}
	assert.NotNil(t, v.Engine())
}

func TestValidate_DefaultEntry(t *testing.T) {
	// 子包未注入时默认走 DefaultValidator
	require.Error(t, Validate(&validReq{Name: "x"}))
	require.NoError(t, Validate(&validReq{Name: "x", Email: "y@z.com"}))
}

func TestSetValidateFn_HookAndRestore(t *testing.T) {
	defer SetValidateFn(nil) // 结束恢复默认

	var called bool
	SetValidateFn(func(any) error { called = true; return nil })

	require.NoError(t, Validate(&validReq{Name: "x"})) // hook 生效，跳过真实校验
	assert.True(t, called)

	// 恢复默认后校验恢复
	SetValidateFn(nil)
	require.Error(t, Validate(&validReq{Name: "x"}))

	// 显式传 nil 同恢复默认
	SetValidateFn(nil)
	require.Error(t, Validate(&validReq{Name: "x"}))
}
