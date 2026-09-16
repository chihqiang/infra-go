package cast

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Generic address-of Ptr tests ---

func TestPtr(t *testing.T) {
	p := Ptr("default")
	require.NotNil(t, p)
	assert.Equal(t, "default", *p)

	pn := Ptr(42)
	require.NotNil(t, pn)
	assert.Equal(t, 42, *pn)
}

// --- Safe dereference Val tests ---

func TestVal(t *testing.T) {
	// non-nil: returns the value the pointer points to
	p := Ptr(42)
	assert.Equal(t, 42, Val(p))

	// nil without a default: returns the zero value
	assert.Equal(t, 0, Val[int](nil))

	// nil with a default provided: returns that default
	assert.Equal(t, 7, Val[int](nil, 7))

	// string cases
	assert.Equal(t, "", Val[string](nil))
	assert.Equal(t, "fb", Val[string](nil, "fb"))
	s := Ptr("ok")
	assert.Equal(t, "ok", Val(s))
}

// --- ToXxxPtr success/failure tests ---

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
	// a failed conversion always returns nil, which makes the result directly usable for
	// optional pointer fields
	assert.Nil(t, ToIntPtr("abc"))
	assert.Nil(t, ToInt64Ptr("abc"))
	assert.Nil(t, ToUintPtr(-1))
	assert.Nil(t, ToUint64Ptr(-1))
	assert.Nil(t, ToFloat32Ptr("abc"))
	assert.Nil(t, ToFloat64Ptr("abc"))
	assert.Nil(t, ToBoolPtr("not a bool"))
	assert.Nil(t, ToDurationPtr("not a duration"))
	assert.Nil(t, ToTimePtr("not a time"))
	// ToString rarely fails, but an unsupported func value can trigger a failure
	assert.Nil(t, ToStringPtr(func() {}))
}
