package websocket

import "encoding/json"

// Event WebSocket 事件，用于事件驱动的消息通信。
// 客户端发送 JSON 格式 {"type": "event_name", "data": {...}}，
// EventHandler 会根据 type 字段分发到对应的处理器。
//
// 用法（发送端）：
//
//	conn.Emit("chat", map[string]string{"msg": "hello"})
//	// 发送: {"type":"chat","data":{"msg":"hello"}}
type Event struct {
	// Type 事件类型。
	Type string `json:"type"`
	// Data 事件数据（原始 JSON）。
	Data json.RawMessage `json:"data,omitempty"`
}

// NewEvent 创建一个事件，data 会被 JSON 序列化。
// 如果 data 为 nil，则 Data 字段为空。
func NewEvent(eventType string, data any) (Event, error) {
	e := Event{Type: eventType}
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return Event{}, err
		}
		e.Data = b
	}
	return e, nil
}

// MustNewEvent 创建一个事件，序列化失败时 panic。
func MustNewEvent(eventType string, data any) Event {
	e, err := NewEvent(eventType, data)
	if err != nil {
		panic(err)
	}
	return e
}

// Decode 将事件的 Data 字段反序列化到 v 中。
func (e Event) Decode(v any) error {
	if len(e.Data) == 0 {
		return nil
	}
	return json.Unmarshal(e.Data, v)
}
