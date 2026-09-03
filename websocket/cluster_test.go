package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mockPubSub 测试 ---

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

// --- 集群跨实例广播测试 ---

func joinHandler() *EventHandler {
	h := NewEventHandler()
	h.Handle("join", func(conn *Conn, data json.RawMessage) {
		var room string
		_ = json.Unmarshal(data, &room)
		conn.Join(room)
		conn.Emit("joined", room)
	})
	return h
}

func TestServer_ClusterBroadcast_SingleServer(t *testing.T) {
	ps := newMockPubSub()

	handler := joinHandler()

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
	handler1 := joinHandler()
	handler2 := joinHandler()

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
