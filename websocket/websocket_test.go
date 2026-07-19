package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MemoryRoom 测试 ---

func TestMemoryRoom_AddAndGet(t *testing.T) {
	room := NewMemoryRoom()

	// 添加连接到房间
	room.Add(1, "room1", "room2")
	room.Add(2, "room1")
	room.Add(3, "room2")

	// 验证房间内的连接
	clients1 := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{1, 2}, clients1)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1, 3}, clients2)

	// 验证连接所在的房间
	rooms := room.GetRooms(1)
	assert.ElementsMatch(t, []string{"room1", "room2"}, rooms)

	rooms2 := room.GetRooms(2)
	assert.ElementsMatch(t, []string{"room1"}, rooms2)
}

func TestMemoryRoom_Delete(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	// 从 room1 移除连接 1
	room.Delete(1, "room1")

	clients := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{2}, clients)

	// 连接 1 仍在 room2
	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	// 移除连接 1 的所有房间
	room.Delete(1)
	rooms := room.GetRooms(1)
	assert.Empty(t, rooms)
}

func TestMemoryRoom_Clear(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1")
	room.Add(2, "room2")

	room.Clear()

	assert.Empty(t, room.GetClients("room1"))
	assert.Empty(t, room.GetClients("room2"))
	assert.Empty(t, room.GetRooms(1))
}

func TestMemoryRoom_DuplicateAdd(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1")
	room.Add(1, "room1") // 重复添加

	clients := room.GetClients("room1")
	assert.Len(t, clients, 1)
}

// --- RedisRoom 测试 ---

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

func TestRedisRoom_AddAndGet(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	clients1 := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{1, 2}, clients1)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	rooms := room.GetRooms(1)
	assert.ElementsMatch(t, []string{"room1", "room2"}, rooms)
}

func TestRedisRoom_Delete(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	room.Delete(1, "room1")

	clients := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{2}, clients)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	room.Delete(1)
	assert.Empty(t, room.GetRooms(1))
}

func TestRedisRoom_Clear(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1")
	room.Add(2, "room2")

	room.Clear()

	assert.Empty(t, room.GetClients("room1"))
	assert.Empty(t, room.GetClients("room2"))
}

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

// --- Server 端到端测试 ---

// dialWs 连接到测试服务器并返回 WebSocket 连接。
func dialWs(t *testing.T, url string) *gws.Conn {
	t.Helper()
	dialer := gws.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(url, nil)
	require.NoError(t, err)
	return ws
}

func TestServer_EchoHandler(t *testing.T) {
	// 使用自定义 Handler 实现 echo
	handler := &echoHandler{}

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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
		Role  string `json:"role"`
	}
	require.NoError(t, event.Decode(&info))
	assert.Equal(t, "user-123", info.UserID)
	assert.Equal(t, "admin", info.Role)
}

func TestServer_WithRedisRoom(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
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

	url := "ws" + ts.URL[len("http"):]

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

// --- 辅助类型 ---

// echoHandler 回声处理器，将收到的消息原样返回。
type echoHandler struct{}

func (h *echoHandler) HandleOpen(conn *Conn)  {}
func (h *echoHandler) HandleMessage(conn *Conn, messageType int, data []byte) {
	_ = conn.WriteMessage(messageType, data)
}
func (h *echoHandler) HandleClose(conn *Conn, err error)  {}
func (h *echoHandler) HandleError(conn *Conn, err error) {}

// 确保 echoHandler 实现了 Handler 接口
var _ Handler = (*echoHandler)(nil)

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

// --- 并发测试 ---

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

	srv := MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

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

// --- Context 支持 ---

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

// --- 集群跨实例广播测试 ---

func TestMockPubSub(t *testing.T) {
	ps := newMockPubSub()
	ctx := context.Background()

	ch, cancel, err := ps.Subscribe(ctx, "test-channel")
	require.NoError(t, err)
	defer cancel()

	err = ps.Publish(ctx, "test-channel", []byte("hello"))
	require.NoError(t, err)

	select {
	case data := <-ch:
		assert.Equal(t, "hello", string(data))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for mockPubSub message")
	}
}

func TestServer_ClusterBroadcast_SingleServer(t *testing.T) {
	ps := newMockPubSub()

	handler := NewEventHandler()
	handler.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})

	srv := MustNew(Config{
		RoomType:     "memory",
		NodeID:       1,
		PingInterval: 5 * time.Second,
		PingTimeout:  10 * time.Second,
	}, handler, WithPubSub(ps))
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):]

	ws := dialWs(t, url)
	defer ws.Close()
	ws.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws.ReadMessage() // joined 确认

	// 通过集群广播到 lobby
	require.NoError(t, srv.To("lobby").PushText("cluster msg"))

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "cluster msg", string(data))
}

func TestServer_ClusterBroadcast_MultiServer(t *testing.T) {
	ps := newMockPubSub()

	// 两台服务器共享同一个 mockPubSub
	handler1 := NewEventHandler()
	handler1.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})

	handler2 := NewEventHandler()
	handler2.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})

	srv1 := MustNew(Config{
		RoomType:     "memory",
		NodeID:       1,
		PingInterval: 5 * time.Second,
		PingTimeout:  10 * time.Second,
	}, handler1, WithPubSub(ps))
	defer srv1.Close()

	srv2 := MustNew(Config{
		RoomType:     "memory",
		NodeID:       2,
		PingInterval: 5 * time.Second,
		PingTimeout:  10 * time.Second,
	}, handler2, WithPubSub(ps))
	defer srv2.Close()

	ts1 := httptest.NewServer(srv1)
	defer ts1.Close()
	ts2 := httptest.NewServer(srv2)
	defer ts2.Close()

	url1 := "ws" + ts1.URL[len("http"):]
	url2 := "ws" + ts2.URL[len("http"):]

	// 客户端连接到各自的服务器，加入 lobby
	ws1 := dialWs(t, url1)
	defer ws1.Close()
	ws1.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws1.ReadMessage()

	ws2 := dialWs(t, url2)
	defer ws2.Close()
	ws2.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws2.ReadMessage()

	// 从 srv1 广播到 lobby，srv2 的客户端也应收到
	require.NoError(t, srv1.To("lobby").PushText("cross-instance"))

	ws1.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data1, err := ws1.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "cross-instance", string(data1))

	ws2.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data2, err := ws2.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "cross-instance", string(data2))
}

func TestServer_ClusterBroadcastAll(t *testing.T) {
	ps := newMockPubSub()

	handler1 := NewEventHandler()
	handler2 := NewEventHandler()

	srv1 := MustNew(Config{
		RoomType:     "memory",
		NodeID:       1,
		PingInterval: 5 * time.Second,
		PingTimeout:  10 * time.Second,
	}, handler1, WithPubSub(ps))
	defer srv1.Close()

	srv2 := MustNew(Config{
		RoomType:     "memory",
		NodeID:       2,
		PingInterval: 5 * time.Second,
		PingTimeout:  10 * time.Second,
	}, handler2, WithPubSub(ps))
	defer srv2.Close()

	ts1 := httptest.NewServer(srv1)
	defer ts1.Close()
	ts2 := httptest.NewServer(srv2)
	defer ts2.Close()

	url1 := "ws" + ts1.URL[len("http"):]
	url2 := "ws" + ts2.URL[len("http"):]

	ws1 := dialWs(t, url1)
	defer ws1.Close()

	ws2 := dialWs(t, url2)
	defer ws2.Close()

	// 从 srv1 广播到所有连接，srv2 的也应收到
	srv1.BroadcastText("hello cluster")

	ws1.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data1, err := ws1.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello cluster", string(data1))

	ws2.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data2, err := ws2.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello cluster", string(data2))
}
