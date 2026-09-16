package websocket

import (
	"encoding/json"
	"sync"
)

// Message type constants (as defined by RFC 6455, matching gorilla/websocket).
const (
	// TextMessage is a text message.
	TextMessage = 1
	// BinaryMessage is a binary message.
	BinaryMessage = 2
	// CloseMessage is a close message.
	CloseMessage = 3
	// PingMessage is a ping message.
	PingMessage = 9
	// PongMessage is a pong message.
	PongMessage = 10
)

// Handler is the WebSocket connection lifecycle handler interface.
// Implement it to handle connection events, or use the default EventHandler.
type Handler interface {
	// HandleOpen is called when the connection is established.
	HandleOpen(conn *Conn)
	// HandleMessage is called when a message arrives.
	// messageType is one of TextMessage, BinaryMessage, etc.
	HandleMessage(conn *Conn, messageType int, data []byte)
	// HandleClose is called when the connection is closed.
	// err is the close reason and is nil for a normal close.
	HandleClose(conn *Conn, err error)
	// HandleError is called when an error occurs.
	HandleError(conn *Conn, err error)
}

// EventHandler is the default event-driven handler.
// It decodes text messages into Event{type, data} automatically and dispatches them
// to the registered handlers based on type.
//
// Usage:
//
//	h := websocket.NewEventHandler()
//	h.OnOpen(func(conn *websocket.Conn) {
//	    log.Printf("connection opened: %d", conn.ID())
//	})
//	h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
//	    var msg struct{ Text string `json:"text"` }
//	    json.Unmarshal(data, &msg)
//	    // broadcast to everyone
//	    conn.Server().Broadcast([]byte(msg.Text))
//	})
//	h.OnClose(func(conn *websocket.Conn, err error) {
//	    log.Printf("connection closed: %d", conn.ID())
//	})
type EventHandler struct {
	mu             sync.RWMutex
	onOpenHandler  func(*Conn)
	onCloseHandler func(*Conn, error)
	onErrorHandler func(*Conn, error)
	onRawMessage   func(*Conn, []byte)
	handlers       map[string]func(*Conn, json.RawMessage)
}

// NewEventHandler creates an event-driven handler.
func NewEventHandler() *EventHandler {
	return &EventHandler{
		handlers: make(map[string]func(*Conn, json.RawMessage)),
	}
}

// OnOpen sets the connection-opened callback; it is chainable.
func (h *EventHandler) OnOpen(fn func(*Conn)) *EventHandler {
	h.mu.Lock()
	h.onOpenHandler = fn
	h.mu.Unlock()
	return h
}

// OnClose sets the connection-closed callback; it is chainable.
func (h *EventHandler) OnClose(fn func(*Conn, error)) *EventHandler {
	h.mu.Lock()
	h.onCloseHandler = fn
	h.mu.Unlock()
	return h
}

// OnError sets the error callback; it is chainable.
func (h *EventHandler) OnError(fn func(*Conn, error)) *EventHandler {
	h.mu.Lock()
	h.onErrorHandler = fn
	h.mu.Unlock()
	return h
}

// OnMessage sets the raw message callback.
// It is invoked for every message (before event dispatch) and is chainable.
func (h *EventHandler) OnMessage(fn func(*Conn, []byte)) *EventHandler {
	h.mu.Lock()
	h.onRawMessage = fn
	h.mu.Unlock()
	return h
}

// Handle registers an event handler.
// When a message in the form {"type": eventType, "data": ...} arrives, the matching
// handler is invoked. It is chainable.
func (h *EventHandler) Handle(eventType string, fn func(*Conn, json.RawMessage)) *EventHandler {
	h.mu.Lock()
	h.handlers[eventType] = fn
	h.mu.Unlock()
	return h
}

// --- Handler interface implementation ---

// HandleOpen implements the Handler interface.
func (h *EventHandler) HandleOpen(conn *Conn) {
	h.mu.RLock()
	fn := h.onOpenHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn)
	}
}

// HandleMessage implements the Handler interface.
// It first invokes the raw message callback, then tries to decode text messages into
// an Event and dispatch them.
func (h *EventHandler) HandleMessage(conn *Conn, messageType int, data []byte) {
	h.mu.RLock()
	rawHandler := h.onRawMessage
	h.mu.RUnlock()
	if rawHandler != nil {
		rawHandler(conn, data)
	}

	// Only text messages take part in event dispatch
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

// HandleClose implements the Handler interface.
func (h *EventHandler) HandleClose(conn *Conn, err error) {
	h.mu.RLock()
	fn := h.onCloseHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn, err)
	}
}

// HandleError implements the Handler interface.
func (h *EventHandler) HandleError(conn *Conn, err error) {
	h.mu.RLock()
	fn := h.onErrorHandler
	h.mu.RUnlock()
	if fn != nil {
		fn(conn, err)
	}
}
