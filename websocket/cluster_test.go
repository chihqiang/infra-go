package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mockPubSub (test only) ---

// mockPubSub is an in-memory PubSub implementation used by the cluster tests.
type mockPubSub struct {
	mu   sync.Mutex
	subs map[string][]chan []byte
}

func newMockPubSub() *mockPubSub {
	return &mockPubSub{
		subs: make(map[string][]chan []byte),
	}
}

func (m *mockPubSub) Publish(_ context.Context, channel string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subs[channel] {
		select {
		case ch <- message:
		default:
		}
	}
	return nil
}

func (m *mockPubSub) Subscribe(_ context.Context, channel string) (<-chan []byte, func(), error) {
	m.mu.Lock()
	ch := make(chan []byte, 100)
	m.subs[channel] = append(m.subs[channel], ch)
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// Mark the subscription as cancelled with nil so that nothing is sent on a
		// closed channel
		for i, sub := range m.subs[channel] {
			if sub == ch {
				m.subs[channel][i] = nil
				break
			}
		}
		close(ch)
	}

	return ch, cancel, nil
}

// --- mockPubSub tests ---

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

// --- Cluster cross-instance broadcast tests ---

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
	_, _, _ = ws.ReadMessage() // joined confirmation

	// Broadcast to lobby through the cluster
	require.NoError(t, srv.To("lobby").PushText("cluster msg"))

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "cluster msg", string(data))
}

func TestServer_ClusterBroadcast_MultiServer(t *testing.T) {
	ps := newMockPubSub()

	// The two servers share the same mockPubSub
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

	// Each client connects to its own server and joins lobby
	ws1 := dialWs(t, url1)
	defer ws1.Close()
	ws1.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws1.ReadMessage()

	ws2 := dialWs(t, url2)
	defer ws2.Close()
	ws2.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, _ = ws2.ReadMessage()

	// Broadcast to lobby from srv1; the client of srv2 must receive it too
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

	// Broadcast to all connections from srv1; srv2's connections must receive it too
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
