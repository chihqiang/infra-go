package websocket

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	gws "github.com/gorilla/websocket"
)

// ErrConnClosed 连接已关闭。
var ErrConnClosed = errors.New("websocket: connection closed")

// Conn 封装 gorilla/websocket.Conn，提供线程安全的写入和连接管理。
//
// 主要特性：
//   - 每个连接拥有全局唯一的 ConnID
//   - 所有写操作通过 mutex 保证线程安全（gorilla/websocket 不允许并发写入）
//   - 支持用户自定义值存储（Set/Get），方便在 Handler 中传递上下文
//   - 支持 Emit 事件发送（JSON 格式 {type, data}）
//   - 支持 Join/Leave 房间
type Conn struct {
	id      ConnID
	server  *Server
	ws      *gws.Conn
	mu      sync.Mutex // 保护 ws 写入
	closed  atomic.Bool
	done    chan struct{} // Close 时关闭，用于通知 ping goroutine 退出
	request *http.Request // 原始 HTTP 请求（只读）
	values  sync.Map      // 用户自定义值
}

// ID 返回连接的唯一 ID。
func (c *Conn) ID() ConnID {
	return c.id
}

// Server 返回所属的服务器实例。
func (c *Conn) Server() *Server {
	return c.server
}

// Request 返回升级时的原始 HTTP 请求。
// 可在 OnOpen 中用于获取查询参数、请求头等。
func (c *Conn) Request() *http.Request {
	return c.request
}

// RemoteAddr 返回客户端地址。
func (c *Conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

// IsClosed 返回连接是否已关闭。
func (c *Conn) IsClosed() bool {
	return c.closed.Load()
}

// --- 写入方法 ---

// WriteMessage 写入指定类型的 WebSocket 消息。
// messageType 取值为 TextMessage、BinaryMessage 等。
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	if c.closed.Load() {
		return ErrConnClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return ErrConnClosed
	}
	return c.ws.WriteMessage(messageType, data)
}

// WriteText 写入文本消息。
func (c *Conn) WriteText(data []byte) error {
	return c.WriteMessage(TextMessage, data)
}

// WriteTextString 写入字符串文本消息。
func (c *Conn) WriteTextString(data string) error {
	return c.WriteMessage(TextMessage, []byte(data))
}

// WriteBinary 写入二进制消息。
func (c *Conn) WriteBinary(data []byte) error {
	return c.WriteMessage(BinaryMessage, data)
}

// WriteJSON 写入 JSON 消息（文本类型）。
func (c *Conn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMessage(TextMessage, data)
}

// Emit 发送事件消息（JSON 格式 {"type":..., "data":...}）。
//
// 用法：
//
//	conn.Emit("chat", map[string]string{"msg": "hello"})
//	// 客户端收到: {"type":"chat","data":{"msg":"hello"}}
func (c *Conn) Emit(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return c.WriteJSON(e)
}

// Push 向当前连接推送消息（WriteText 的别名）。
func (c *Conn) Push(data []byte) error {
	return c.WriteText(data)
}

// --- 房间操作 ---

// Join 将当前连接加入房间。
func (c *Conn) Join(rooms ...string) {
	c.server.room.Add(c.id, rooms...)
}

// Leave 将当前连接从房间移除。
// 如果 rooms 为空，则离开所有房间。
func (c *Conn) Leave(rooms ...string) {
	c.server.room.Delete(c.id, rooms...)
}

// Rooms 返回当前连接所在的所有房间名称。
func (c *Conn) Rooms() []string {
	return c.server.room.GetRooms(c.id)
}

// --- 用户值 ---

// Set 设置用户自定义值。
func (c *Conn) Set(key string, value any) {
	c.values.Store(key, value)
}

// Get 获取用户自定义值。
func (c *Conn) Get(key string) (any, bool) {
	return c.values.Load(key)
}

// MustGet 获取用户自定义值，不存在时返回 nil。
func (c *Conn) MustGet(key string) any {
	v, _ := c.values.Load(key)
	return v
}

// --- 关闭 ---

// Close 关闭连接。
// 内部使用原子操作保证只关闭一次，同时通知 ping goroutine 退出。
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
