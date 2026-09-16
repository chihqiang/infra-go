package cast

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ToIntSlice tests ---

func TestToIntSlice(t *testing.T) {
	// []int
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]int{1, 2, 3}))

	// []any
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]any{1, 2, 3}))

	// []string
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice([]string{"1", "2", "3"}))

	// comma-separated string
	assert.Equal(t, []int{1, 2, 3}, ToIntSlice("1,2,3"))

	// empty string
	assert.Equal(t, []int{}, ToIntSlice(""))

	// nil
	assert.Equal(t, []int{}, ToIntSlice(nil))
}

func TestToIntSliceE_Error(t *testing.T) {
	_, err := ToIntSliceE([]string{"1", "abc"})
	assert.Error(t, err)
}

func TestToIntSliceE_MoreErrors(t *testing.T) {
	// []any containing a non-convertible element
	_, err := ToIntSliceE([]any{1, "abc"})
	assert.Error(t, err)

	// comma string containing an invalid numeric field
	_, err = ToIntSliceE("1,abc")
	assert.Error(t, err)

	// unsupported type
	_, err = ToIntSliceE(map[string]int{})
	assert.Error(t, err)
}

// --- ToStringSlice tests ---

func TestToStringSlice(t *testing.T) {
	// []string
	assert.Equal(t, []string{"a", "b"}, ToStringSlice([]string{"a", "b"}))

	// []any
	assert.Equal(t, []string{"1", "2"}, ToStringSlice([]any{1, 2}))

	// comma-separated string
	assert.Equal(t, []string{"a", "b", "c"}, ToStringSlice("a,b,c"))

	// []byte
	assert.Equal(t, []string{"hello"}, ToStringSlice([]byte("hello")))

	// empty string
	assert.Equal(t, []string{}, ToStringSlice(""))

	// nil
	assert.Equal(t, []string{}, ToStringSlice(nil))
}

func TestToStringSliceE_Errors(t *testing.T) {
	// []any containing a non-stringifiable element (func fails to marshal)
	_, err := ToStringSliceE([]any{func() {}})
	assert.Error(t, err)

	// unsupported type
	_, err = ToStringSliceE(map[string]string{})
	assert.Error(t, err)
}
