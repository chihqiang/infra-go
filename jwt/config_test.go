package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Token type constant tests ---

func TestTokenTypeConstants(t *testing.T) {
	assert.Equal(t, "access", TokenTypeAccess)
	assert.Equal(t, "refresh", TokenTypeRefresh)
}

// --- Algorithm constant tests ---

func TestAlgorithmConstants(t *testing.T) {
	assert.Equal(t, Algorithm("HS256"), AlgorithmHS256)
	assert.Equal(t, Algorithm("HS384"), AlgorithmHS384)
	assert.Equal(t, Algorithm("HS512"), AlgorithmHS512)
}
