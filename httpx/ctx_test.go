package httpx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- 请求 ID 测试 ---

func TestContextWithRequestID_RoundTrip(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	assert.Equal(t, "req-123", RequestIDFromContext(ctx))
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	assert.Equal(t, "", RequestIDFromContext(context.Background()))
	assert.Equal(t, "", RequestIDFromContext(nil))
}
