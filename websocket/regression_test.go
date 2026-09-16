package websocket

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers regression tests for the following three defects:
//  1. Writes had no write deadline → slow clients could block broadcasts/Close forever;
//  2. The consumer goroutine leaked on Redis PubSub cancel (subscription not released);
//  3. Server.Close cleared room keys shared by all instances → wiping other nodes' rooms.

// --- 1. Write timeout ---

// TestFillDefault_WriteTimeout verifies that WriteTimeout has a default value.
func TestFillDefault_WriteTimeout(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, 10*time.Second, c.WriteTimeout)

	// An explicit value is preserved
	c = fillDefault(Config{WriteTimeout: 3 * time.Second})
	assert.Equal(t, 3*time.Second, c.WriteTimeout)
}

// TestConn_WriteTimeoutResolution verifies the write timeout resolution rules.
func TestConn_WriteTimeoutResolution(t *testing.T) {
	t.Run("no server uses fallback", func(t *testing.T) {
		c := &Conn{}
		assert.Equal(t, defaultWriteTimeout, c.writeTimeout())
	})

	t.Run("zero config uses fallback", func(t *testing.T) {
		c := &Conn{server: &Server{}}
		assert.Equal(t, defaultWriteTimeout, c.writeTimeout())
	})

	t.Run("negative disables", func(t *testing.T) {
		c := &Conn{server: &Server{cfg: Config{WriteTimeout: -1}}}
		assert.Equal(t, time.Duration(0), c.writeTimeout())
	})

	t.Run("explicit value", func(t *testing.T) {
		c := &Conn{server: &Server{cfg: Config{WriteTimeout: 2 * time.Second}}}
		assert.Equal(t, 2*time.Second, c.writeTimeout())
	})
}

// TestConn_WriteTimesOutForNonReadingClient is a regression test: writing to a client
// that does not read data must fail within the write timeout instead of blocking forever.
//
// Historical defect: WriteMessage did not set a write deadline, so once the client TCP
// buffer was full the write blocked forever, and it held the connection write lock while
// blocking, which also blocked broadcasts, Conn.Close, the heartbeat and Server.Close.
func TestConn_WriteTimesOutForNonReadingClient(t *testing.T) {
	handler := NewEventHandler()
	opened := make(chan *Conn, 1)
	handler.OnOpen(func(conn *Conn) { opened <- conn })

	srv := MustNew(Config{
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
		WriteTimeout: 100 * time.Millisecond, // trigger the timeout
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ws := dialWs(t, wsURL(ts))
	defer ws.Close()
	// Note: nothing is read on purpose, simulating a slow/stuck client

	var conn *Conn
	select {
	case conn = <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not registered")
	}

	// Keep writing 1MB chunks until it errors. The client does not read → the buffer
	// fills up → the write deadline kicks in.
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ {
			if err := conn.WriteMessage(BinaryMessage, chunk); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		require.Error(t, err, "writing to a non-reading client must fail, not block")
		t.Logf("write failed as expected: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("write blocked forever: no write deadline was applied to the connection")
	}
}

// TestConn_CloseNotBlockedByWrite verifies that Close is not stuck on the write lock
// after a write fails.
func TestConn_CloseNotBlockedByWrite(t *testing.T) {
	handler := NewEventHandler()
	opened := make(chan *Conn, 1)
	handler.OnOpen(func(conn *Conn) { opened <- conn })

	srv := MustNew(Config{
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
		WriteTimeout: 50 * time.Millisecond,
	}, handler)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ws := dialWs(t, wsURL(ts))
	defer ws.Close()

	var conn *Conn
	select {
	case conn = <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not registered")
	}

	chunk := bytes.Repeat([]byte("y"), 1<<20)
	go func() {
		for i := 0; i < 64; i++ {
			if err := conn.WriteMessage(BinaryMessage, chunk); err != nil {
				return
			}
		}
	}()

	// Call Close while a write is in flight; it must return in bounded time
	done := make(chan error, 1)
	go func() { done <- conn.Close() }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked: write lock held by an unbounded write")
	}

	assert.True(t, conn.IsClosed())
	_ = srv.Close()
}

// --- 2. PubSub cancel does not leak ---

// TestRedisPubSub_CancelClosesChannel is a regression test: after cancel the subscribe
// goroutine must exit and close the output channel.
//
// Historical defect: the consumer goroutine blocked in pubsub.Receive(ctx) while cancel
// only closed done, so the goroutine never exited and the subscription connection was
// never released; the output channel also stayed open (each cluster Stop leaked
// 1 goroutine + 1 Redis subscription connection).
func TestRedisPubSub_CancelClosesChannel(t *testing.T) {
	mr, client := newMiniRedis(t)
	defer client.Close()
	_ = mr

	ps := NewRedisPubSub(client)
	ch, cancel, err := ps.Subscribe(context.Background(), "cancel-test")
	require.NoError(t, err)

	cancel()

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "output channel must be closed after cancel")
	case <-time.After(3 * time.Second):
		t.Fatal("subscribe goroutine did not exit after cancel (goroutine + connection leak)")
	}
}

// TestRedisPubSub_CancelIsIdempotent verifies that cancel can be called repeatedly
// (cluster Stop and the caller may each call it once).
func TestRedisPubSub_CancelIsIdempotent(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	ps := NewRedisPubSub(client)
	ch, cancel, err := ps.Subscribe(context.Background(), "cancel-twice")
	require.NoError(t, err)

	require.NotPanics(t, func() {
		cancel()
		cancel()
		cancel()
	})

	select {
	case _, ok := <-ch:
		assert.False(t, ok)
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after repeated cancel")
	}
}

// TestRedisPubSub_MessagesStillDeliveredAfterFix verifies that the normal path is unaffected.
func TestRedisPubSub_MessagesStillDeliveredAfterFix(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	ctx := context.Background()
	ps := NewRedisPubSub(client)
	ch, cancel, err := ps.Subscribe(ctx, "deliver-test")
	require.NoError(t, err)
	defer cancel()

	require.NoError(t, ps.Publish(ctx, "deliver-test", []byte("hello")))

	select {
	case data, ok := <-ch:
		require.True(t, ok)
		assert.Equal(t, "hello", string(data))
	case <-time.After(3 * time.Second):
		t.Fatal("message not delivered")
	}
}

// TestClusterHandler_StopReleasesSubscription verifies that the subscription channel is
// closed after the cluster is stopped.
func TestClusterHandler_StopReleasesSubscription(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	handler := NewEventHandler()
	srv := MustNew(Config{
		RoomType:     "redis",
		NodeID:       1,
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
	}, handler, WithRedisClient(client), WithPubSub(NewRedisPubSub(client)))
	defer srv.Close()

	require.NoError(t, srv.cluster.Start())

	// Stop must return in bounded time (it must not block on a subscription goroutine
	// that never exits)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.cluster.Stop()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ClusterHandler.Stop blocked")
	}
}

// --- 3. Server.Close does not affect other nodes ---

// TestServer_CloseDoesNotClearOtherNodesRooms is a regression test: after a single instance
// shuts down, the connections of the other instances (nodes) must stay in the room.
//
// Historical defect: Server.Close called room.Clear(), while the keys of RedisRoom are
// shared by all instances, so clearing them wiped the room memberships of the other nodes
// and made their later broadcasts fail silently.
func TestServer_CloseDoesNotClearOtherNodesRooms(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	// Simulate the connections of an "other node" in these two rooms
	const foreignFD ConnID = 999999
	room.Add(foreignFD, "lobby", "news")

	handler := joinHandler()
	srv := MustNew(Config{
		RoomType:     "redis",
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
	}, handler, WithRedisClient(client))

	ts := httptest.NewServer(srv)
	url := wsURL(ts)

	ws := dialWs(t, url)
	ws.WriteJSON(MustNewEvent("join", "lobby"))
	_, _, err := ws.ReadMessage() // joined confirmation
	require.NoError(t, err)

	// Both nodes (this instance + the simulated node) are in lobby
	require.Len(t, room.GetClients("lobby"), 2, "both nodes should be in the room")

	require.NoError(t, srv.Close())
	require.NoError(t, ws.Close())
	ts.Close()

	// The room membership of the other node must be preserved
	lobby := room.GetClients("lobby")
	assert.Equal(t, []ConnID{foreignFD}, lobby,
		"only the other node's connection should remain in the room")

	news := room.GetClients("news")
	assert.Equal(t, []ConnID{foreignFD}, news, "unrelated rooms must be untouched")
}

// TestServer_CloseRemovesOwnConnectionsFromRoom verifies that this instance's connections
// are really removed from the room.
func TestServer_CloseRemovesOwnConnectionsFromRoom(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	srv := MustNew(Config{
		RoomType:     "redis",
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
	}, joinHandler(), WithRedisClient(client))

	ts := httptest.NewServer(srv)
	url := wsURL(ts)

	ws := dialWs(t, url)
	ws.WriteJSON(MustNewEvent("join", "room-a"))
	_, _, err := ws.ReadMessage()
	require.NoError(t, err)

	require.Len(t, room.GetClients("room-a"), 1)

	require.NoError(t, srv.Close())
	require.NoError(t, ws.Close())
	ts.Close()

	assert.Empty(t, room.GetClients("room-a"),
		"this instance's connections must be removed on close")
}

// TestRedisRoom_DeleteOnlyAffectsGivenFD verifies that Delete only affects the mapping of
// the given connection.
func TestRedisRoom_DeleteOnlyAffectsGivenFD(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")
	room.Add(1, "shared")
	room.Add(2, "shared")

	room.Delete(1)

	assert.Equal(t, []ConnID{2}, room.GetClients("shared"))
	assert.Empty(t, room.GetRooms(1))
	assert.Equal(t, []string{"shared"}, room.GetRooms(2))
}
