package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialWs connects to the test server and returns the WebSocket connection.
func dialWs(t *testing.T, url string) *gws.Conn {
	t.Helper()
	dialer := gws.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(url, nil)
	require.NoError(t, err)
	return ws
}

// echoHandler is an echo handler that writes received messages back unchanged.
type echoHandler struct{}

func (h *echoHandler) HandleOpen(conn *Conn) {}
func (h *echoHandler) HandleMessage(conn *Conn, messageType int, data []byte) {
	_ = conn.WriteMessage(messageType, data)
}
func (h *echoHandler) HandleClose(conn *Conn, err error) {}
func (h *echoHandler) HandleError(conn *Conn, err error) {}

// Ensure that echoHandler implements the Handler interface
var _ Handler = (*echoHandler)(nil)

func newTestServer(handler Handler) *Server {
	return MustNew(Config{
		PingInterval: 1 * time.Second,
		PingTimeout:  2 * time.Second,
	}, handler)
}

// wsURL returns the WebSocket URL of the test server.
func wsURL(ts *httptest.Server) string {
	return "ws" + ts.URL[len("http"):]
}

// waitForOpens waits until the server has registered n connections (HandleOpen is called
// after conns.Store), so that a test does not fail intermittently when a broadcast happens
// before registration.
func waitForOpens(t *testing.T, opened <-chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-opened:
		case <-time.After(3 * time.Second):
			t.Fatalf("server registered %d/%d connections in time", i, n)
		}
	}
}

// --- Server end-to-end tests ---

func TestServer_EchoHandler(t *testing.T) {
	// Use a custom Handler to implement echo
	handler := &echoHandler{}

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws := dialWs(t, url)
	defer ws.Close()

	// Send a message
	require.NoError(t, ws.WriteMessage(gws.TextMessage, []byte("hello")))

	// Read the echo back
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestServer_RoomBroadcast(t *testing.T) {
	handler := NewEventHandler()

	// When the "join" event arrives, add the connection to the room
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

	// Client 1 joins room1
	ws1 := dialWs(t, url)
	defer ws1.Close()
	ws1.WriteJSON(MustNewEvent("join", "room1"))

	// Wait for the confirmation
	_, _, _ = ws1.ReadMessage()

	// Client 2 joins room1
	ws2 := dialWs(t, url)
	defer ws2.Close()
	ws2.WriteJSON(MustNewEvent("join", "room1"))
	_, _, _ = ws2.ReadMessage()

	// Broadcast a message to room1
	require.NoError(t, srv.To("room1").PushText("broadcast msg"))

	// Both clients must receive the message
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

	// A completed client handshake does not mean the server has registered the connection
	// in conns: ServeHTTP stores it before HandleOpen, so OnOpen is used as the signal
	// that registration is done; otherwise the broadcast could happen before registration
	// and the connection would miss the message (an intermittent test timeout).
	opened := make(chan struct{}, 2)
	handler.OnOpen(func(conn *Conn) { opened <- struct{}{} })

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	url := wsURL(ts)

	ws1 := dialWs(t, url)
	defer ws1.Close()

	ws2 := dialWs(t, url)
	defer ws2.Close()

	waitForOpens(t, opened, 2)

	// Broadcast to all connections
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

	// Broadcast to the lobby room
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
		// Concurrent writes
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

	// Verify that a context can be passed in to control the lifecycle (future extension)
	assert.NotNil(t, ctx)
}

// --- Cluster ID uniqueness tests ---

// nodeIDFromConnID extracts the node ID from a connection ID, used to assert how
// nextConnID encodes it.
func nodeIDFromConnID(id ConnID) uint16 {
	return uint16(id >> 32)
}

// localIDFromConnID extracts the local counter part from a connection ID.
func localIDFromConnID(id ConnID) uint32 {
	return uint32(id & 0xFFFFFFFF)
}

func TestServer_ConnID_UniqueSingleNode(t *testing.T) {
	srv := MustNew(Config{}, NewEventHandler())
	defer srv.Close()

	id1 := srv.nextConnID()
	id2 := srv.nextConnID()
	id3 := srv.nextConnID()

	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id2, id3)
	assert.Equal(t, uint16(0), nodeIDFromConnID(id1)) // default NodeID=0
	assert.Equal(t, uint32(1), localIDFromConnID(id1))
	assert.Equal(t, uint32(2), localIDFromConnID(id2))
	assert.Equal(t, uint32(3), localIDFromConnID(id3))
}

func TestServer_ConnID_ClusterUniqueness(t *testing.T) {
	// Simulate two nodes
	srv1 := MustNew(Config{NodeID: 1}, NewEventHandler())
	defer srv1.Close()

	srv2 := MustNew(Config{NodeID: 2}, NewEventHandler())
	defer srv2.Close()

	// Generate IDs on each node
	id1a := srv1.nextConnID()
	id1b := srv1.nextConnID()
	id2a := srv2.nextConnID()
	id2b := srv2.nextConnID()

	// The high 32 bits of node 1's IDs are 1
	assert.Equal(t, uint16(1), nodeIDFromConnID(id1a))
	assert.Equal(t, uint16(1), nodeIDFromConnID(id1b))

	// The high 32 bits of node 2's IDs are 2
	assert.Equal(t, uint16(2), nodeIDFromConnID(id2a))
	assert.Equal(t, uint16(2), nodeIDFromConnID(id2b))

	// Globally unique: IDs of different nodes never overlap
	assert.NotEqual(t, id1a, id2a)
	assert.NotEqual(t, id1a, id2b)
	assert.NotEqual(t, id1b, id2a)

	// IDs of the same node increase locally
	assert.True(t, localIDFromConnID(id1b) > localIDFromConnID(id1a))
	assert.True(t, localIDFromConnID(id2b) > localIDFromConnID(id2a))
}

func TestServer_NodeID(t *testing.T) {
	srv := MustNew(Config{NodeID: 42}, NewEventHandler())
	defer srv.Close()

	assert.Equal(t, uint16(42), srv.NodeID())
}

// panicOpenHandler panics in OnOpen after joining a room.
type panicOpenHandler struct{}

func (h *panicOpenHandler) HandleOpen(conn *Conn)            { conn.Join("lobby"); panic("boom") }
func (h *panicOpenHandler) HandleMessage(*Conn, int, []byte) {}
func (h *panicOpenHandler) HandleClose(*Conn, error)         {}
func (h *panicOpenHandler) HandleError(*Conn, error)         {}

// TestServer_OnOpenPanicCleansUp is a regression test: the cleanup defer was
// registered after HandleOpen, so an OnOpen panic leaked the connection entry, the
// room membership and the heartbeat goroutine.
func TestServer_OnOpenPanicCleansUp(t *testing.T) {
	srv := newTestServer(&panicOpenHandler{})
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()

	require.Eventually(t, func() bool { return srv.Count() == 0 }, 2*time.Second, 10*time.Millisecond,
		"the connection must be unregistered after an OnOpen panic")
	assert.Empty(t, srv.Room().GetClients("lobby"), "room membership must be removed")
}

// TestServer_HandlerPanicClosesConnection verifies that a panic in HandleMessage is
// contained instead of escaping into net/http.
func TestServer_HandlerPanicClosesConnection(t *testing.T) {
	handler := NewEventHandler()
	handler.Handle("explode", func(*Conn, json.RawMessage) { panic("boom") })
	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()
	require.NoError(t, client.WriteMessage(gws.TextMessage, []byte(`{"type":"explode"}`)))

	require.Eventually(t, func() bool { return srv.Count() == 0 }, 2*time.Second, 10*time.Millisecond)
}

// TestServer_CloseConnSendsCloseFrame is a regression test: Conn.Close only closed
// the TCP socket, so clients saw 1006 abnormal closure instead of the close code.
func TestServer_CloseConnSendsCloseFrame(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()
	require.Eventually(t, func() bool { return srv.Count() == 1 }, 2*time.Second, 10*time.Millisecond)

	var id ConnID
	srv.conns.Range(func(key, _ any) bool {
		id = key.(ConnID)
		return false
	})
	require.NoError(t, srv.CloseConn(id))

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := client.ReadMessage()
	var closeErr *gws.CloseError
	require.ErrorAs(t, err, &closeErr)
	assert.Equal(t, gws.CloseNormalClosure, closeErr.Code)
}

// TestServer_NormalCloseDoesNotCallOnError verifies that a client-initiated normal
// close reaches OnClose (with a nil reason) and not OnError.
func TestServer_NormalCloseDoesNotCallOnError(t *testing.T) {
	handler := NewEventHandler()
	closed := make(chan error, 1)
	errs := make(chan error, 1)
	handler.OnClose(func(_ *Conn, err error) { closed <- err })
	handler.OnError(func(_ *Conn, err error) { errs <- err })

	srv := newTestServer(handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()

	require.NoError(t, client.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, "bye")))
	// Complete the closing handshake so the read side can finish.
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = client.ReadMessage()

	select {
	case err := <-closed:
		assert.NoError(t, err, "HandleClose must receive a nil reason for a normal close")
	case <-time.After(2 * time.Second):
		t.Fatal("HandleClose was not called")
	}
	select {
	case err := <-errs:
		t.Fatalf("HandleError must not be called for a normal close: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestServer_DefaultCheckOriginRejectsCrossOrigin is a regression test: the default
// CheckOrigin returned true, which allowed Cross-Site WebSocket Hijacking.
func TestServer_DefaultCheckOriginRejectsCrossOrigin(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	dialer := gws.Dialer{HandshakeTimeout: 2 * time.Second}
	header := http.Header{}
	header.Set("Origin", "https://evil.example")
	conn, resp, err := dialer.Dial(wsURL(ts), header)
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Same-origin handshakes keep working.
	header.Set("Origin", ts.URL)
	conn, resp, err = dialer.Dial(wsURL(ts), header)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = conn.Close()
}

// TestNewConfigClampsNonPositiveHeartbeat is a regression test: a negative
// PingInterval survived fillDefault and time.NewTicker panicked inside the
// heartbeat goroutine, killing the process.
func TestNewConfigClampsNonPositiveHeartbeat(t *testing.T) {
	srv, err := New(Config{PingInterval: -time.Second, PingTimeout: -time.Second}, &echoHandler{})
	require.NoError(t, err)
	defer srv.Close()

	assert.Equal(t, 25*time.Second, srv.Config().PingInterval)
	assert.Equal(t, 60*time.Second, srv.Config().PingTimeout)
}
