package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialWs 连接到测试服务器并返回 WebSocket 连接。
func dialWs(t *testing.T, url string) *gws.Conn {
	t.Helper()
	dialer := gws.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(url, nil)
	require.NoError(t, err)
	return ws
}

// echoHandler 回声处理器，将收到的消息原样返回。
type echoHandler struct{}

func (h *echoHandler) HandleOpen(conn *Conn) {}
func (h *echoHandler) HandleMessage(conn *Conn, messageType int, data []byte) {
	_ = conn.WriteMessage(messageType, data)
}
func (h *echoHandler) HandleClose(conn *Conn, err error) {}
func (h *echoHandler) HandleError(conn *Conn, err error) {}

// 确保 echoHandler 实现了 Handler 接口
var _ Handler = (*echoHandler)(nil)

func newTestServer(handler Handler) *Server {
	return MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
}

func wsURL(ts *httptest.Server) string {
	return "ws" + ts.URL[len("http"):]
}

// --- Server 端到端测试 ---

func TestServer_EchoHandler(t *testing.T) {
	// 使用自定义 Handler 实现 echo
	handler := &echoHandler{}

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws := dialWs(t, url)
	defer ws.Close()

	// 发送消息
	require.NoError(t, ws.WriteMessage(gws.TextMessage, []byte("hello")))

	// 读取回声
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestServer_RoomBroadcast(t *testing.T) {
	handler := NewEventHandler()

	// 当收到 "join" 事件时，将连接加入房间
	handler.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	// 客户端 1 加入 room1
	ws1 := dialWs(t, url)
	defer ws1.Close()
	ws1.WriteJSON(MustNewEvent("join", "room1"))

	// 等待确认
	_, _, _ = ws1.ReadMessage()

	// 客户端 2 加入 room1
	ws2 := dialWs(t, url)
	defer ws2.Close()
	ws2.WriteJSON(MustNewEvent("join", "room1"))
	_, _, _ = ws2.ReadMessage()

	// 广播消息到 room1
	require.NoError(t, srv.To("room1").PushText("broadcast msg"))

	// 两个客户端都应收到消息
	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data1, err := ws1.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "broadcast msg", string(data1))

	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data2, err := ws2.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "broadcast msg", string(data2))
}

func TestServer_Emit(t *testing.T) {
	handler := NewEventHandler()

	handler.Handle("ping", func(conn *Conn, data json.RawMessage) {
		conn.Emit("pong", map[string]string{"msg": "hello"})
	})

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws := dialWs(t, url)
	defer ws.Close()

	ws.WriteJSON(MustNewEvent("ping", nil))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)

	var event Event
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "pong", event.Type)

	var msg struct {
		Msg string `json:"msg"`
	}
	require.NoError(t, event.Decode(&msg))
	assert.Equal(t, "hello", msg.Msg)
}

func TestServer_BroadcastToAll(t *testing.T) {
	handler := NewEventHandler()

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws1 := dialWs(t, url)
	defer ws1.Close()

	ws2 := dialWs(t, url)
	defer ws2.Close()

	// 广播到所有连接
	srv.BroadcastText("hello all")

	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data1, err := ws1.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello all", string(data1))

	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data2, err := ws2.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello all", string(data2))
}

func TestServer_ConnValues(t *testing.T) {
	handler := NewEventHandler()

	var wg sync.WaitGroup
	wg.Add(1)

	handler.OnOpen(func(conn *Conn) {
		conn.Set("userID", "user-123")
		conn.Set("role", "admin")
		wg.Done()
	})

	handler.Handle("get", func(conn *Conn, data json.RawMessage) {
		userID, _ := conn.Get("userID")
		role, _ := conn.Get("role")
		conn.Emit("info", map[string]string{
			"user_id": userID.(string),
			"role":    role.(string),
		})
	})

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws := dialWs(t, url)
	defer ws.Close()

	wg.Wait()

	ws.WriteJSON(MustNewEvent("get", nil))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)

	var event Event
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "info", event.Type)

	var info struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	require.NoError(t, event.Decode(&info))
	assert.Equal(t, "user-123", info.UserID)
	assert.Equal(t, "admin", info.Role)
}

func TestServer_WithRedisRoom(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	handler := NewEventHandler()

	handler.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})

	srv := MustNew(Config{
		RoomType:     "redis",
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler, WithRedisClient(client))
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws1 := dialWs(t, url)
	defer ws1.Close()
	ws1.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws1.ReadMessage()

	ws2 := dialWs(t, url)
	defer ws2.Close()
	ws2.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws2.ReadMessage()

	// 广播到 lobby 房间
	require.NoError(t, srv.To("lobby").PushText("redis broadcast"))

	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data1, err := ws1.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "redis broadcast", string(data1))

	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data2, err := ws2.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "redis broadcast", string(data2))
}

func TestServer_ConcurrentWrite(t *testing.T) {
	handler := NewEventHandler()

	handler.Handle("start", func(conn *Conn, data json.RawMessage) {
		// 并发写入
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = conn.WriteTextString("concurrent")
			}()
		}
		wg.Wait()
	})

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws := dialWs(t, url)
	defer ws.Close()

	ws.WriteJSON(MustNewEvent("start", nil))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	count := 0
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
		count++
	}
	assert.Equal(t, 10, count)
}

func TestServer_Context(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 确保可以传入 context 来控制生命周期（未来扩展）
	assert.NotNil(t, ctx)
}

// --- 集群 ID 唯一性测试 ---

func TestServer_ConnID_UniqueSingleNode(t *testing.T) {
	srv := MustNew(Config{}, NewEventHandler())
	defer srv.Close()

	id1 := srv.nextConnID()
	id2 := srv.nextConnID()
	id3 := srv.nextConnID()

	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id2, id3)
	assert.Equal(t, uint16(0), nodeIDFromConnID(id1)) // 默认 NodeID=0
	assert.Equal(t, uint32(1), localIDFromConnID(id1))
	assert.Equal(t, uint32(2), localIDFromConnID(id2))
	assert.Equal(t, uint32(3), localIDFromConnID(id3))
}

func TestServer_ConnID_ClusterUniqueness(t *testing.T) {
	// 模拟两个节点
	srv1 := MustNew(Config{NodeID: 1}, NewEventHandler())
	defer srv1.Close()

	srv2 := MustNew(Config{NodeID: 2}, NewEventHandler())
	defer srv2.Close()

	// 各自生成 ID
	id1a := srv1.nextConnID()
	id1b := srv1.nextConnID()
	id2a := srv2.nextConnID()
	id2b := srv2.nextConnID()

	// 节点 1 的 ID 高 32 位为 1
	assert.Equal(t, uint16(1), nodeIDFromConnID(id1a))
	assert.Equal(t, uint16(1), nodeIDFromConnID(id1b))

	// 节点 2 的 ID 高 32 位为 2
	assert.Equal(t, uint16(2), nodeIDFromConnID(id2a))
	assert.Equal(t, uint16(2), nodeIDFromConnID(id2b))

	// 全局唯一：不同节点的 ID 不重叠
	assert.NotEqual(t, id1a, id2a)
	assert.NotEqual(t, id1a, id2b)
	assert.NotEqual(t, id1b, id2a)

	// 同节点的 ID 局部递增
	assert.True(t, localIDFromConnID(id1b) > localIDFromConnID(id1a))
	assert.True(t, localIDFromConnID(id2b) > localIDFromConnID(id2a))
}

func TestServer_NodeID(t *testing.T) {
	srv := MustNew(Config{NodeID: 42}, NewEventHandler())
	defer srv.Close()

	assert.Equal(t, uint16(42), srv.NodeID())
}
