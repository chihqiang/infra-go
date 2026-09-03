package cast

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ToString 测试 ---

func TestToString(t *testing.T) {
	assert.Equal(t, "hello", ToString("hello"))
	assert.Equal(t, "42", ToString(42))
	assert.Equal(t, "42", ToString(int64(42)))
	assert.Equal(t, "42", ToString(uint(42)))
	assert.Equal(t, "3.14", ToString(3.14))
	assert.Equal(t, "true", ToString(true))
	assert.Equal(t, "hello", ToString([]byte("hello")))
	assert.Equal(t, "42", ToString(json.Number("42")))
	assert.Equal(t, "", ToString(nil))
}

func TestToString_Stringer(t *testing.T) {
	// error 类型实现 fmt.Stringer，应转为错误消息
	assert.Equal(t, "custom", ToString(errors.New("custom")))
}
