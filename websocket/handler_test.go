package websocket

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- EventHandler 测试 ---

func TestEventHandler_EventDispatch(t *testing.T) {
	h := NewEventHandler()

	var mu sync.Mutex
	var receivedType string
	var receivedData string

	h.Handle("greeting", func(conn *Conn, data json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		receivedType = "greeting"
		_ = json.Unmarshal(data, &receivedData)
	})

	// 模拟收到事件消息
	event := MustNewEvent("greeting", "hello world")
	data, _ := json.Marshal(event)

	// EventHandler.OnMessage 不需要真实连接
	h.HandleMessage(nil, TextMessage, data)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "greeting", receivedType)
	assert.Equal(t, "hello world", receivedData)
}

func TestEventHandler_RawMessage(t *testing.T) {
	h := NewEventHandler()

	var mu sync.Mutex
	var received []byte

	h.OnMessage(func(conn *Conn, data []byte) {
		mu.Lock()
		defer mu.Unlock()
		received = data
	})

	h.HandleMessage(nil, TextMessage, []byte("raw message"))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []byte("raw message"), received)
}

func TestEventHandler_IgnoreNonText(t *testing.T) {
	h := NewEventHandler()

	called := false
	h.Handle("test", func(conn *Conn, data json.RawMessage) {
		called = true
	})

	// 二进制消息不应触发事件分发
	event := MustNewEvent("test", "data")
	data, _ := json.Marshal(event)
	h.HandleMessage(nil, BinaryMessage, data)

	assert.False(t, called)
}
