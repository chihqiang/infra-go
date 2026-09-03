package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- 令牌类型常量测试 ---

func TestTokenTypeConstants(t *testing.T) {
	assert.Equal(t, "access", TokenTypeAccess)
	assert.Equal(t, "refresh", TokenTypeRefresh)
}

// --- 算法常量测试 ---

func TestAlgorithmConstants(t *testing.T) {
	assert.Equal(t, Algorithm("HS256"), AlgorithmHS256)
	assert.Equal(t, Algorithm("HS384"), AlgorithmHS384)
	assert.Equal(t, Algorithm("HS512"), AlgorithmHS512)
}
