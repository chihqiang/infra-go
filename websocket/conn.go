package websocket

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	gws "github.com/gorilla/websocket"
)

// ErrConnClosed means the connection is already closed.
var ErrConnClosed = errors.New("websocket: connection closed")

// defaultWriteTimeout is the write timeout fallback used when server is nil
// (e.g. a Conn built by hand in tests).
const defaultWriteTimeout = 10 * time.Second

// Conn wraps gorilla/websocket.Conn and provides thread-safe writes and connection
// management.
//
// Main features:
//   - Every connection has a globally unique ConnID
//   - All writes are guarded by a mutex (gorilla/websocket forbids concurrent writes)
//   - User-defined values can be stored (Set/Get) to pass context inside a Handler
//   - Events can be emitted (JSON format {type, data})
//   - Rooms can be joined/left
type Conn struct {
	id      ConnID
	server  *Server
	ws      *gws.Conn
	mu      sync.Mutex // guards writes to ws
	closed  atomic.Bool
	done    chan struct{} // closed by Close to signal the ping goroutine to exit
	request *http.Request // original HTTP request (read-only)
	values  sync.Map      // user-defined values
}

// ID returns the unique ID of the connection.
func (c *Conn) ID() ConnID {
	return c.id
}

// Server returns the owning server instance.
func (c *Conn) Server() *Server {
	return c.server
}

// Request returns the original HTTP request used for the upgrade.
// It can be used in OnOpen to read query parameters, request headers, etc.
func (c *Conn) Request() *http.Request {
	return c.request
}

// RemoteAddr returns the client address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

// IsClosed reports whether the connection has been closed.
func (c *Conn) IsClosed() bool {
	return c.closed.Load()
}

// --- Write methods ---

// writeTimeout returns the per-write timeout of this connection.
// A return value of 0 means unlimited (only when the config is explicitly negative).
func (c *Conn) writeTimeout() time.Duration {
	if c.server == nil {
		// A hand-built Conn (test scenario) has no config, so use the fallback
		return defaultWriteTimeout
	}
	switch {
	case c.server.cfg.WriteTimeout < 0:
		// The write timeout is explicitly disabled
		return 0
	case c.server.cfg.WriteTimeout == 0:
		// Fallback for a config that did not go through fillDefault, so that it is
		// not mistaken for "unlimited"
		return defaultWriteTimeout
	default:
		return c.server.cfg.WriteTimeout
	}
}

// WriteMessage writes a WebSocket message of the given type.
// messageType is one of TextMessage, BinaryMessage, etc.
//
// A write deadline is set before writing (see Config.WriteTimeout) so that a slow
// client cannot block the write forever and, with it, broadcasts and shutdown.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	if c.closed.Load() {
		return ErrConnClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return ErrConnClosed
	}
	if timeout := c.writeTimeout(); timeout > 0 {
		_ = c.ws.SetWriteDeadline(time.Now().Add(timeout))
	}
	return c.ws.WriteMessage(messageType, data)
}

// WriteText writes a text message.
func (c *Conn) WriteText(data []byte) error {
	return c.WriteMessage(TextMessage, data)
}

// WriteTextString writes a string text message.
func (c *Conn) WriteTextString(data string) error {
	return c.WriteMessage(TextMessage, []byte(data))
}

// WriteBinary writes a binary message.
func (c *Conn) WriteBinary(data []byte) error {
	return c.WriteMessage(BinaryMessage, data)
}

// WriteJSON writes a JSON message (text type).
func (c *Conn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMessage(TextMessage, data)
}

// Emit sends an event message (JSON format {"type":..., "data":...}).
//
// Usage:
//
//	conn.Emit("chat", map[string]string{"msg": "hello"})
//	// the client receives: {"type":"chat","data":{"msg":"hello"}}
func (c *Conn) Emit(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return c.WriteJSON(e)
}

// Push pushes a message to the current connection (an alias of WriteText).
func (c *Conn) Push(data []byte) error {
	return c.WriteText(data)
}

// --- Room operations ---

// Join adds the current connection to the given rooms.
func (c *Conn) Join(rooms ...string) {
	c.server.room.Add(c.id, rooms...)
}

// Leave removes the current connection from the given rooms.
// If rooms is empty, the connection leaves all rooms.
func (c *Conn) Leave(rooms ...string) {
	c.server.room.Delete(c.id, rooms...)
}

// Rooms returns the names of all rooms the current connection is in.
func (c *Conn) Rooms() []string {
	return c.server.room.GetRooms(c.id)
}

// --- User values ---

// Set stores a user-defined value.
func (c *Conn) Set(key string, value any) {
	c.values.Store(key, value)
}

// Get loads a user-defined value.
func (c *Conn) Get(key string) (any, bool) {
	return c.values.Load(key)
}

// MustGet loads a user-defined value and returns nil when it does not exist.
func (c *Conn) MustGet(key string) any {
	v, _ := c.values.Load(key)
	return v
}

// --- Close ---

// Close closes the connection.
// An atomic operation makes sure it only closes once and it also signals the ping
// goroutine to exit.
func (c *Conn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	c.mu.Lock()
	err := c.ws.Close()
	c.mu.Unlock()
	return err
}
