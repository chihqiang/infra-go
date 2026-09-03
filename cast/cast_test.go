package cast

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ErrCastFailed 测试 ---

func TestErrCastFailed(t *testing.T) {
	_, err := ToIntE("abc")
	assert.Error(t, err)

	var castErr *ErrCastFailed
	assert.True(t, errors.As(err, &castErr))
	assert.Equal(t, "string", castErr.From)
	assert.Equal(t, "int", castErr.To)
	assert.Contains(t, err.Error(), "cast")
	assert.Contains(t, err.Error(), "string")
	assert.Contains(t, err.Error(), "int")
}
