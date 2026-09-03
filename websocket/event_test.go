package websocket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Event 测试 ---

func TestEvent_NewEvent(t *testing.T) {
	e, err := NewEvent("test", map[string]int{"a": 1})
	require.NoError(t, err)
	assert.Equal(t, "test", e.Type)

	var m map[string]int
	require.NoError(t, e.Decode(&m))
	assert.Equal(t, 1, m["a"])
}

func TestEvent_DecodeEmpty(t *testing.T) {
	e := Event{Type: "test"}
	var m map[string]int
	assert.NoError(t, e.Decode(&m))
	assert.Nil(t, m)
}
