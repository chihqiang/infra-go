package binding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers validator.go: DefaultValidator, SetValidateFn/validate/Validate.

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
	require.Error(t, v.ValidateStruct(&validReq{Name: "Alice"})) // email is missing
}

func TestDefaultValidator_ValueNotPointer(t *testing.T) {
	v := &DefaultValidator{}
	require.NoError(t, v.ValidateStruct(validReq{Name: "Alice", Email: "a@b.com"}))
}

func TestDefaultValidator_SliceAndArray(t *testing.T) {
	v := &DefaultValidator{}
	// all valid → nil
	require.NoError(t, v.ValidateStruct([]validReq{
		{Name: "a", Email: "a@b.com"},
		{Name: "b", Email: "c@d.com"},
	}))
	// contains an invalid element → error
	require.Error(t, v.ValidateStruct([]validReq{
		{Name: "a", Email: "a@b.com"},
		{Name: "b"},
	}))
	// the same applies to arrays
	require.Error(t, v.ValidateStruct([1]validReq{{Name: "b"}}))
}

func TestDefaultValidator_NilAndScalar(t *testing.T) {
	v := &DefaultValidator{}
	require.NoError(t, v.ValidateStruct(nil))
	require.NoError(t, v.ValidateStruct(42))           // non-struct types are skipped directly
	require.NoError(t, v.ValidateStruct("plain text")) // Ptr → non-struct → keep dereferencing down to string
}

func TestDefaultValidator_Engine(t *testing.T) {
	v := &DefaultValidator{}
	assert.NotNil(t, v.Engine())
}

func TestValidate_DefaultEntry(t *testing.T) {
	// When no sub-package hook is installed, validation falls back to DefaultValidator
	require.Error(t, Validate(&validReq{Name: "x"}))
	require.NoError(t, Validate(&validReq{Name: "x", Email: "y@z.com"}))
}

func TestSetValidateFn_HookAndRestore(t *testing.T) {
	defer SetValidateFn(nil) // restore the default when the test ends

	var called bool
	SetValidateFn(func(any) error { called = true; return nil })

	require.NoError(t, Validate(&validReq{Name: "x"})) // the hook takes effect and skips real validation
	assert.True(t, called)

	// Validation is restored after restoring the default
	SetValidateFn(nil)
	require.Error(t, Validate(&validReq{Name: "x"}))

	// Explicitly passing nil also restores the default
	SetValidateFn(nil)
	require.Error(t, Validate(&validReq{Name: "x"}))
}
