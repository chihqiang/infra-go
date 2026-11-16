package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chihqiang/infra-go/core/logger"
)

// firstConn returns the only registered connection.
func firstConn(t *testing.T, srv *Server) *Conn {
	t.Helper()
	require.Eventually(t, func() bool { return srv.Count() == 1 }, 2*time.Second, 10*time.Millisecond)

	var conn *Conn
	srv.conns.Range(func(_, v any) bool {
		conn = v.(*Conn)
		return false
	})
	require.NotNil(t, conn)
	return conn
}

func TestConnAccessorsAndWriteHelpers(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()

	conn := firstConn(t, srv)
	assert.Same(t, srv, conn.Server())
	assert.NotNil(t, conn.Request())
	assert.NotNil(t, conn.RemoteAddr())
	assert.False(t, conn.IsClosed())

	conn.Set("user", "alice")
	v, ok := conn.Get("user")
	assert.True(t, ok)
	assert.Equal(t, "alice", v)
	assert.Nil(t, conn.MustGet("missing"))

	conn.Join("room-a", "room-b")
	assert.ElementsMatch(t, []string{"room-a", "room-b"}, conn.Rooms())

	// Binary and text helpers.
	require.NoError(t, conn.WriteBinary([]byte("bin")))
	messageType, data, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, BinaryMessage, messageType)
	assert.Equal(t, "bin", string(data))

	require.NoError(t, conn.Push([]byte("push")))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "push", string(data))

	require.NoError(t, conn.WriteJSON(map[string]string{"k": "v"}))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "v", decoded["k"])

	require.NoError(t, conn.Emit("chat", map[string]string{"text": "hi"}))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	var event Event
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "chat", event.Type)

	conn.Leave("room-a")
	assert.Equal(t, []string{"room-b"}, conn.Rooms())
	conn.Leave()
	assert.Empty(t, conn.Rooms())

	require.NoError(t, srv.CloseConn(conn.ID()))
	require.Eventually(t, func() bool { return srv.Count() == 0 }, 2*time.Second, 10*time.Millisecond)
}

func TestServerBroadcastHelpers(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()
	conn := firstConn(t, srv)
	conn.Join("all")

	require.NoError(t, srv.BroadcastJSON(map[string]string{"k": "v"}))
	_, data, err := client.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"k":"v"}`, string(data))

	require.NoError(t, srv.BroadcastEvent("notice", "maintenance"))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	var event Event
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "notice", event.Type)
}

func TestBroadcasterHelpers(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()
	conn := firstConn(t, srv)
	conn.Join("room")

	// Push (alias of PushText), PushBinary, PushText.
	require.NoError(t, srv.To("room").Push([]byte("push")))
	_, data, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "push", string(data))

	require.NoError(t, srv.To("room").PushText("text"))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "text", string(data))

	require.NoError(t, srv.To("room").PushBinary([]byte("binary")))
	messageType, data, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, BinaryMessage, messageType)
	assert.Equal(t, "binary", string(data))

	require.NoError(t, srv.To("room").WriteJSON(map[string]int{"n": 1}))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(data))

	require.NoError(t, srv.To("room").Emit("evt", "payload"))
	_, data, err = client.ReadMessage()
	require.NoError(t, err)
	var event Event
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "evt", event.Type)

	// Marshal failures surface as errors.
	assert.Error(t, srv.To("room").WriteJSON(make(chan int)))
	assert.Error(t, srv.To("room").Emit("evt", make(chan int)))
}

func TestBroadcasterReportsClosedConnection(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	defer client.Close()
	conn := firstConn(t, srv)
	conn.Join("room")

	// Simulate a connection that is already unusable (for example after a failed
	// write); broadcasting to it must surface the error instead of silently dropping
	// the message.
	conn.closed.Store(true)
	assert.Error(t, srv.To("room").PushText("x"))
}

func TestServerOptions(t *testing.T) {
	l := logger.New(logger.Config{})
	origin := func(*http.Request) bool { return false }
	srv, err := New(Config{}, &echoHandler{}, WithLogger(l), WithCheckOrigin(origin), WithSubprotocols("chat"))
	require.NoError(t, err)
	defer srv.Close()

	assert.Same(t, l, srv.logger)
	assert.False(t, srv.upgrader.CheckOrigin(nil))
	assert.Equal(t, []string{"chat"}, srv.upgrader.Subprotocols)
}

func TestServerConnectionLookupErrors(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()

	_, ok := srv.GetConn(ConnID(999))
	assert.False(t, ok)
	assert.Error(t, srv.CloseConn(ConnID(999)))
}

type panicCallbacksHandler struct{}

func (panicCallbacksHandler) HandleOpen(*Conn)                 {}
func (panicCallbacksHandler) HandleMessage(*Conn, int, []byte) {}
func (panicCallbacksHandler) HandleClose(*Conn, error)         { panic("close panic") }
func (panicCallbacksHandler) HandleError(*Conn, error)         { panic("error panic") }

// TestServerCallbackPanicsAreContained covers the panic guards of HandleClose and
// HandleError: a user callback panic must not escape the connection goroutine.
func TestServerCallbackPanicsAreContained(t *testing.T) {
	srv := newTestServer(panicCallbacksHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// A normal close reaches HandleClose, which panics.
	client := dialWs(t, wsURL(ts))
	firstConn(t, srv)
	require.NoError(t, client.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, "bye")))
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = client.ReadMessage()
	require.Eventually(t, func() bool { return srv.Count() == 0 }, 2*time.Second, 10*time.Millisecond)
	_ = client.Close()

	// An abrupt close reaches HandleError, which also panics.
	client2 := dialWs(t, wsURL(ts))
	conn2 := firstConn(t, srv)
	_ = client2.Close()
	require.Eventually(t, func() bool { return conn2.IsClosed() }, 2*time.Second, 10*time.Millisecond)
}

// TestConnectionClosedByHeartbeat covers the ping write failure path.
func TestConnectionClosedByHeartbeat(t *testing.T) {
	srv := MustNew(Config{
		PingInterval: 20 * time.Millisecond,
		PingTimeout:  200 * time.Millisecond,
	}, &echoHandler{})
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := dialWs(t, wsURL(ts))
	conn := firstConn(t, srv)
	require.NoError(t, client.Close())

	require.Eventually(t, func() bool { return conn.IsClosed() }, 3*time.Second, 20*time.Millisecond,
		"a heartbeat write to a closed client must close the connection")
	require.Eventually(t, func() bool { return srv.Count() == 0 }, 3*time.Second, 20*time.Millisecond)
}

// TestNewAppliesDefaultsForZeroConfig keeps Config normalization covered.
func TestNewAppliesDefaultsForZeroConfig(t *testing.T) {
	srv, err := New(Config{}, &echoHandler{})
	require.NoError(t, err)
	defer srv.Close()
	assert.Equal(t, 25*time.Second, srv.Config().PingInterval)
	assert.Equal(t, 60*time.Second, srv.Config().PingTimeout)
	assert.Equal(t, 4096, srv.Config().ReadBufferSize)
	assert.Equal(t, int64(4096), srv.Config().MaxMessageSize)
	assert.Equal(t, "memory", srv.Config().RoomType)
	assert.Equal(t, "ws:room:", srv.Config().RoomPrefix)
	assert.Equal(t, uint16(0), srv.NodeID())
}

func TestServerBroadcastWithoutConnections(t *testing.T) {
	srv := newTestServer(&echoHandler{})
	defer srv.Close()
	require.NoError(t, srv.BroadcastJSON(map[string]string{"k": "v"}))
	require.NoError(t, srv.BroadcastEvent("evt", nil))
	assert.NoError(t, srv.To("empty").PushText("x"))
}
