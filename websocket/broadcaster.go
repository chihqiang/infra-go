package websocket

import (
	"context"
	"encoding/json"
	"fmt"
)

// Broadcaster broadcasts messages to every connection in the given rooms.
// It is created by Server.To() and supports a fluent API.
//
// Standalone mode: iterate the local connections in the rooms and write to them.
// Cluster mode: publish the message to all instances over Redis Pub/Sub; every
// instance then dispatches it to its local connections in the target rooms.
//
// Usage:
//
//	// Push a text message to several rooms
//	srv.To("room1", "room2").PushText("hello")
//
//	// Push an event to a room
//	srv.To("room1").Emit("chat", map[string]string{"msg": "hi"})
//
//	// Push JSON data
//	srv.To("room1").WriteJSON(myData)
type Broadcaster struct {
	server  *Server
	targets []string // target rooms
}

// write sends the message to all target rooms.
// In cluster mode it broadcasts over Redis Pub/Sub; in standalone mode it delivers locally.
func (b *Broadcaster) write(messageType int, data []byte) error {
	// Cluster mode: broadcast across instances over Pub/Sub
	if b.server.cluster != nil {
		msg := clusterMessage{
			Type:        clusterMessageTypeRoom,
			MessageType: messageType,
			Data:        data,
			Rooms:       b.targets,
		}
		return b.server.cluster.Publish(context.Background(), msg)
	}

	// Standalone mode: iterate the local connections
	fds := b.collectLocalFDs()
	var errs []string
	sent := 0

	for _, fd := range fds {
		conn, ok := b.server.GetConn(fd)
		if !ok {
			continue
		}
		if err := conn.WriteMessage(messageType, data); err != nil {
			errs = append(errs, fmt.Sprintf("conn %d: %v", fd, err))
			continue
		}
		sent++
	}

	if len(errs) > 0 {
		return fmt.Errorf("websocket: broadcast errors: %v (sent %d/%d)", errs, sent, len(fds))
	}
	return nil
}

// collectLocalFDs collects the unique connection IDs in all target rooms (this instance only).
func (b *Broadcaster) collectLocalFDs() []ConnID {
	fdSet := make(map[ConnID]struct{})
	for _, room := range b.targets {
		for _, fd := range b.server.room.GetClients(room) {
			fdSet[fd] = struct{}{}
		}
	}

	fds := make([]ConnID, 0, len(fdSet))
	for fd := range fdSet {
		fds = append(fds, fd)
	}
	return fds
}

// Push pushes a text message to every connection in the target rooms.
func (b *Broadcaster) Push(data []byte) error {
	return b.write(TextMessage, data)
}

// PushText pushes a string text message to every connection in the target rooms.
func (b *Broadcaster) PushText(data string) error {
	return b.write(TextMessage, []byte(data))
}

// PushBinary pushes a binary message to every connection in the target rooms.
func (b *Broadcaster) PushBinary(data []byte) error {
	return b.write(BinaryMessage, data)
}

// WriteJSON pushes a JSON message to every connection in the target rooms.
func (b *Broadcaster) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.write(TextMessage, data)
}

// Emit pushes an event message to every connection in the target rooms.
// The message format is {"type":..., "data":...}.
func (b *Broadcaster) Emit(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return b.WriteJSON(e)
}
