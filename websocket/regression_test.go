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

// 本文件覆盖针对以下三个缺陷的回归测试：
//  1. 写操作没有写截止时间 → 慢客户端可无限阻塞广播/Close；
//  2. Redis PubSub 取消时消费 goroutine 泄漏（订阅连接不释放）；
//  3. Server.Close 清空所有实例共享的房间键 → 抹掉其他节点的房间关系。

// --- 1. 写超时 ---

// TestFillDefault_WriteTimeout 验证 WriteTimeout 有默认值。
func TestFillDefault_WriteTimeout(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, 10*time.Second, c.WriteTimeout)

	// 显式设置时被保留
	c = fillDefault(Config{WriteTimeout: 3 * time.Second})
	assert.Equal(t, 3*time.Second, c.WriteTimeout)
}

// TestConn_WriteTimeoutResolution 验证写超时的取值规则。
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

// TestConn_WriteTimesOutForNonReadingClient 回归测试：向不读取数据的客户端写入
// 必须在写超时内失败，而不是无限阻塞。
//
// 历史缺陷：WriteMessage 未设置写截止时间，客户端 TCP 缓冲区写满后写入会永久阻塞，
// 且期间持有连接的写锁，连带阻塞广播、Conn.Close、心跳与 Server.Close。
func TestConn_WriteTimesOutForNonReadingClient(t *testing.T) {
	handler := NewEventHandler()
	opened := make(chan *Conn, 1)
	handler.OnOpen(func(conn *Conn) { opened <- conn })

	srv := MustNew(Config{
		PingInterval: 30 * time.Second,
		PingTimeout:  60 * time.Second,
		WriteTimeout: 100 * time.Millisecond, // 触发超时
	}, handler)
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ws := dialWs(t, wsURL(ts))
	defer ws.Close()
	// 注意：故意不读取任何数据，模拟慢/僵死客户端

	var conn *Conn
	select {
	case conn = <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not registered")
	}

	// 持续写入 1MB 分块，直到报错。客户端不读 → 缓冲区写满 → 写截止时间生效。
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

// TestConn_CloseNotBlockedByWrite 验证写失败后 Close 不会被写锁卡住。
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

	// 写操作进行中调用 Close，必须在有限时间内返回
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

// --- 2. PubSub 取消不泄漏 ---

// TestRedisPubSub_CancelClosesChannel 回归测试：cancel 后订阅 goroutine 必须退出并
// 关闭输出通道。
//
// 历史缺陷：消费 goroutine 阻塞在 pubsub.Receive(ctx) 上，cancel 只关闭 done，
// 因此 goroutine 永不退出、订阅连接永不释放，输出通道也永不关闭
// （导致每次集群 Stop 泄漏 1 个 goroutine + 1 条 Redis 订阅连接）。
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

// TestRedisPubSub_CancelIsIdempotent 验证 cancel 可重复调用（集群 Stop 与调用方可能各调一次）。
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

// TestRedisPubSub_MessagesStillDeliveredAfterFix 验证正常路径未受影响。
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

// TestClusterHandler_StopReleasesSubscription 验证集群 Stop 后订阅通道被关闭。
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

	// Stop 必须在有限时间内返回（不会因订阅 goroutine 未退出而阻塞）
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

// --- 3. Server.Close 不影响其他节点 ---

// TestServer_CloseDoesNotClearOtherNodesRooms 回归测试：单个实例关闭后，
// 其他实例（节点）在该房间中的连接关系必须保留。
//
// 历史缺陷：Server.Close 调用 room.Clear()，而 RedisRoom 的键由所有实例共享，
// 清空会抹掉其他节点的房间成员关系，使其他节点的广播静默失效。
func TestServer_CloseDoesNotClearOtherNodesRooms(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	// 模拟"其他节点"在这两个房间中的连接
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
	_, _, err := ws.ReadMessage() // joined 确认
	require.NoError(t, err)

	// 两个节点（本实例 + 模拟节点）都在 lobby 中
	require.Len(t, room.GetClients("lobby"), 2, "both nodes should be in the room")

	require.NoError(t, srv.Close())
	require.NoError(t, ws.Close())
	ts.Close()

	// 其他节点的连接关系必须保留
	lobby := room.GetClients("lobby")
	assert.Equal(t, []ConnID{foreignFD}, lobby,
		"only the other node's connection should remain in the room")

	news := room.GetClients("news")
	assert.Equal(t, []ConnID{foreignFD}, news, "unrelated rooms must be untouched")
}

// TestServer_CloseRemovesOwnConnectionsFromRoom 验证本实例的连接确实被移出房间。
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

// TestRedisRoom_DeleteOnlyAffectsGivenFD 验证 Delete 只影响指定连接的映射。
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
