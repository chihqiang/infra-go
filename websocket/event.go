package websocket

import "encoding/json"

// Event is a WebSocket event used for event-driven messaging.
// The client sends JSON in the form {"type": "event_name", "data": {...}} and
// EventHandler dispatches it to the matching handler based on the type field.
//
// Usage (sender side):
//
//	conn.Emit("chat", map[string]string{"msg": "hello"})
//	// sends: {"type":"chat","data":{"msg":"hello"}}
type Event struct {
	// Type is the event type.
	Type string `json:"type"`
	// Data is the event payload (raw JSON).
	Data json.RawMessage `json:"data,omitempty"`
}

// NewEvent creates an event; data is JSON-marshalled.
// When data is nil the Data field stays empty.
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

// MustNewEvent creates an event and panics when marshalling fails.
func MustNewEvent(eventType string, data any) Event {
	e, err := NewEvent(eventType, data)
	if err != nil {
		panic(err)
	}
	return e
}

// Decode unmarshals the Data field of the event into v.
func (e Event) Decode(v any) error {
	if len(e.Data) == 0 {
		return nil
	}
	return json.Unmarshal(e.Data, v)
}
