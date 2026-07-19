package websocket

import (
	"encoding/json"
	"sync"
)

// 消息类型常量（对应 RFC 6455 定义，与 gorilla/websocket 一致）。
const (
	// TextMessage 文本消息。
	TextMessage = 1
	// BinaryMessage 二进制消息。
	BinaryMessage = 2
	// CloseMessage 关闭消息。
	CloseMessage = 3
	// PingMessage Ping 消息。
	PingMessage = 9
	// PongMessage Pong 消息。
	PongMessage = 10
)

// Handler WebSocket 连接生命周期处理器接口。
// 用户实现此接口来处理连接事件，或使用默认的 EventHandler。
type Handler interface {
	// HandleOpen 连接建立时调用。
	HandleOpen(conn *Conn)
	// HandleMessage 收到消息时调用。
	// messageType 取值为 TextMessage、BinaryMessage 等。
	HandleMessage(conn *Conn, messageType int, data []byte)
	// HandleClose 连接关闭时调用。
	// err 为关闭原因，正常关闭时为 nil。
	HandleClose(conn *Conn, err error)
	// HandleError 发生错误时调用。
	HandleError(conn *Conn, err error)
}

// EventHandler 默认的事件驱动处理器。
// 自动将文本消息解码为 Event{type, data}，并根据 type 分发到注册的处理器。
//
// 用法：
//
//	h := websocket.NewEventHandler()
//	h.OnOpen(func(conn *websocket.Conn) {
//	    log.Printf("连接建立: %d", conn.ID())
//	})
//	h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
//	    var msg struct{ Text string `json:"text"` }
//	    json.Unmarshal(data, &msg)
//	    // 广播给所有人
//	    conn.Server().Broadcast([]byte(msg.Text))
//	})
//	h.OnClose(func(conn *websocket.Conn, err error) {
//	    log.Printf("连接关闭: %d", conn.ID())
//	})
type EventHandler struct {
	mu             sync.RWMutex
	onOpenHandler  func(*Conn)
	onCloseHandler func(*Conn, error)
	onErrorHandler func(*Conn, error)
	onRawMessage    func(*Conn, []byte)
	handlers       map[string]func(*Conn, json.RawMessage)
}

// NewEventHandler 创建一个事件驱动处理器。
func NewEventHandler() *EventHandler {
	return &EventHandler{
		handlers: make(map[string]func(*Conn, json.RawMessage)),
	}
}

// OnOpen 设置连接建立回调，支持链式调用。
func (h *EventHandler) OnOpen(fn func(*Conn)) *EventHandler {
	h.mu.Lock()
	h.onOpenHandler = fn
	h.mu.Unlock()
	return h
}

// OnClose 设置连接关闭回调，支持链式调用。
func (h *EventHandler) OnClose(fn func(*Conn, error)) *EventHandler {
	h.mu.Lock()
	h.onCloseHandler = fn
	h.mu.Unlock()
	return h
}

// OnError 设置错误回调，支持链式调用。
func (h *EventHandler) OnError(fn func(*Conn, error)) *EventHandler {
	h.mu.Lock()
	h.onErrorHandler = fn
	h.mu.Unlock()
	return h
}

// OnMessage 设置原始消息回调。
// 对每条消息（在事件分发之前）调用，支持链式调用。
func (h *EventHandler) OnMessage(fn func(*Conn, []byte)) *EventHandler {
	h.mu.Lock()
	h.onRawMessage = fn
	h.mu.Unlock()
	return h
}

// Handle 注册事件处理器。
// 当收到 {"type": eventType, "data": ...} 格式的消息时，调用对应的处理器。
// 支持链式调用。
func (h *EventHandler) Handle(eventType string, fn func(*Conn, json.RawMessage)) *EventHandler {
	h.mu.Lock()
	h.handlers[eventType] = fn
	h.mu.Unlock()
	return h
}

// --- Handler 接口实现 ---

// HandleOpen 实现 Handler 接口。
func (h *EventHandler) HandleOpen(conn *Conn) {
	h.mu.RLock()
	fn := h.onOpenHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn)
	}
}

// HandleMessage 实现 Handler 接口。
// 先调用原始消息回调，然后尝试将文本消息解码为 Event 并分发。
func (h *EventHandler) HandleMessage(conn *Conn, messageType int, data []byte) {
	h.mu.RLock()
	rawHandler := h.onRawMessage
	h.mu.RUnlock()
	if rawHandler != nil {
		rawHandler(conn, data)
	}

	// 仅处理文本消息的事件分发
	if messageType != TextMessage {
		return
	}

	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}
	if event.Type == "" {
		return
	}

	h.mu.RLock()
	handler, ok := h.handlers[event.Type]
	h.mu.RUnlock()
	if ok {
		handler(conn, event.Data)
	}
}

// HandleClose 实现 Handler 接口。
func (h *EventHandler) HandleClose(conn *Conn, err error) {
	h.mu.RLock()
	fn := h.onCloseHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn, err)
	}
}

// HandleError 实现 Handler 接口。
func (h *EventHandler) HandleError(conn *Conn, err error) {
	h.mu.RLock()
	fn := h.onErrorHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn, err)
	}
}
