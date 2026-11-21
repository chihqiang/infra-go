package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chihqiang/ten-go/core/logger"
	gws "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// Server is the WebSocket server.
// It implements http.Handler and can be used directly with http.HandleFunc or http.Server.
//
// Core responsibilities:
//   - HTTP → WebSocket upgrade
//   - Connection lifecycle management (create, read, close)
//   - Heartbeat detection (Ping/Pong)
//   - Room management
//   - Message broadcast (standalone + cluster)
//
// Usage:
//
//	h := ws.NewEventHandler()
//	h.Handle("chat", func(conn *ws.Conn, data json.RawMessage) {
//	    conn.Server().Broadcast([]byte("new message"))
//	})
//	srv := ws.MustNew(ws.Config{}, h)
//	http.Handle("/ws", srv)
//	http.ListenAndServe(":8080", nil)
type Server struct {
	cfg      Config
	upgrader *gws.Upgrader
	handler  Handler
	room     Room

	conns   sync.Map      // ConnID -> *Conn
	counter atomic.Uint64 // local connection counter (without the NodeID prefix)

	redisClient RedisClient // Redis client created by the Server (must be closed on Close)
	ownRedis    bool        // whether the Redis client was created by the Server
	logger      logger.ILogger

	// Cluster support
	cluster *ClusterHandler // cluster handler; non-nil means cluster mode is enabled
	nodeID  uint16          // node ID

	// closed marks that Close has begun; ServeHTTP refuses to register a new
	// connection afterwards so a connection can never outlive server shutdown.
	closed atomic.Bool
}

// New creates a WebSocket server.
func New(cfg Config, handler Handler, opts ...Option) (*Server, error) {
	c := fillDefault(cfg)

	var opt options
	for _, o := range opts {
		o(&opt)
	}

	// Create the room
	var room Room
	var ownRedis bool
	var redisClient RedisClient
	switch c.RoomType {
	case roomTypeRedis:
		redisClient = opt.redisClient
		if redisClient == nil {
			redisClient = redis.NewClient(&redis.Options{
				Addr:     c.RedisAddr,
				Password: c.RedisPassword,
				DB:       c.RedisDB,
			})
			ownRedis = true
		}
		room = NewRedisRoom(redisClient, c.RoomPrefix)
	default:
		room = NewMemoryRoom()
	}

	// Create the cluster handler
	// It is enabled automatically when RoomType is redis, or when a custom PubSub is
	// passed through WithPubSub
	var cluster *ClusterHandler
	if opt.pubsub != nil {
		cluster = NewClusterHandler(nil, c.NodeID, opt.pubsub)
	} else if c.RoomType == roomTypeRedis && redisClient != nil {
		cluster = NewClusterHandler(nil, c.NodeID, NewRedisPubSub(redisClient))
	}

	// Create the Upgrader
	upgrader := &gws.Upgrader{
		ReadBufferSize:  c.ReadBufferSize,
		WriteBufferSize: c.WriteBufferSize,
		// CheckOrigin stays nil by default: gorilla then applies its same-origin
		// check, preventing Cross-Site WebSocket Hijacking. Returning true here
		// (the old behaviour) let any site open an authenticated connection.
		CheckOrigin: opt.checkOrigin,
	}
	if len(opt.subprotocols) > 0 {
		upgrader.Subprotocols = opt.subprotocols
	}

	l := opt.logger
	if l == nil {
		l = logger.GetGlobal()
	}

	s := &Server{
		cfg:         c,
		upgrader:    upgrader,
		handler:     handler,
		room:        room,
		logger:      l,
		redisClient: redisClient,
		ownRedis:    ownRedis,
		cluster:     cluster,
		nodeID:      c.NodeID,
	}

	// Start listening on the cluster
	if cluster != nil {
		cluster.server = s
		if err := cluster.Start(); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// MustNew creates a WebSocket server and panics on error.
func MustNew(cfg Config, handler Handler, opts ...Option) *Server {
	s, err := New(cfg, handler, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// nextConnID generates a globally unique connection ID.
// In cluster mode it is encoded as nodeID<<32 | localCounter so that IDs of different
// instances never overlap.
func (s *Server) nextConnID() ConnID {
	local := s.counter.Add(1)
	return ConnID(s.nodeID)<<32 | ConnID(local)
}

// --- http.Handler implementation ---

// ServeHTTP handles the HTTP request and upgrades the connection to WebSocket.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("ws: upgrade failed", logger.Err(err))
		return
	}

	// Close may have finished while the upgrade was in flight; a connection
	// registered after Close would never be shut down.
	if s.closed.Load() {
		_ = ws.Close()
		return
	}

	id := s.nextConnID()
	conn := &Conn{
		id:      id,
		server:  s,
		ws:      ws,
		done:    make(chan struct{}),
		request: r,
	}

	// Set the message size limit
	if s.cfg.MaxMessageSize > 0 {
		ws.SetReadLimit(s.cfg.MaxMessageSize)
	}

	// Register in the connection table
	s.conns.Store(id, conn)

	// Re-check after Store: either this sees closed, or Close's Range sees the
	// stored connection, so no connection can slip through shutdown.
	if s.closed.Load() {
		s.conns.Delete(id)
		s.room.Delete(id)
		_ = conn.Close()
		return
	}

	// Teardown is registered before any user callback runs. If OnOpen panics, the
	// old code never reached its defer, leaking the entry in conns, the room
	// membership and the heartbeat goroutine.
	var closeErr error
	defer func() {
		// Stop the heartbeat and the socket first, then unregister. HandleClose is
		// invoked last and its panic is contained so it cannot skip cleanup.
		_ = conn.Close()
		s.conns.Delete(id)
		s.room.Delete(id)
		s.safeHandleClose(conn, closeErr)
	}()

	// Start the heartbeat
	s.startPing(conn)

	// Invoke OnOpen
	if !s.safeHandleOpen(conn) {
		return
	}

	// Read loop
	for {
		// Set the read timeout (based on the heartbeat)
		_ = ws.SetReadDeadline(time.Now().Add(s.cfg.PingTimeout))

		messageType, data, err := ws.ReadMessage()
		if err != nil {
			closeErr = err
			if isNormalClose(err) {
				s.logger.Info("ws: connection closed", logger.Int64("conn_id", int64(id)))
				// A normal close is not an error: OnError must not be called for it.
				closeErr = nil
				return
			}
			var ce *gws.CloseError
			if errors.As(err, &ce) {
				s.logger.Info("ws: connection closed",
					logger.Int64("conn_id", int64(id)),
					logger.Int("code", ce.Code))
			}
			s.safeHandleError(conn, err)
			return
		}

		if !s.safeHandleMessage(conn, messageType, data) {
			closeErr = errHandlerPanic
			return
		}
	}
}

// errHandlerPanic is passed to HandleClose when a message handler panicked.
var errHandlerPanic = errors.New("ws: message handler panicked")

// safeHandleOpen invokes OnOpen, recovering from a panic so teardown always runs.
func (s *Server) safeHandleOpen(conn *Conn) (ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("ws: panic in OnOpen",
				logger.Int64("conn_id", int64(conn.id)),
				logger.Any("panic", recovered))
			ok = false
		}
	}()
	s.handler.HandleOpen(conn)
	return true
}

// safeHandleMessage invokes the message handler; a panic closes the connection
// instead of escaping into net/http.
func (s *Server) safeHandleMessage(conn *Conn, messageType int, data []byte) (ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("ws: panic in HandleMessage",
				logger.Int64("conn_id", int64(conn.id)),
				logger.Int("message_type", messageType),
				logger.Any("panic", recovered))
			ok = false
		}
	}()
	s.handler.HandleMessage(conn, messageType, data)
	return true
}

// safeHandleError invokes OnError; a panic is logged and contained.
func (s *Server) safeHandleError(conn *Conn, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("ws: panic in HandleError",
				logger.Int64("conn_id", int64(conn.id)),
				logger.Any("panic", recovered))
		}
	}()
	s.handler.HandleError(conn, err)
}

// safeHandleClose invokes OnClose; a panic is logged and contained.
func (s *Server) safeHandleClose(conn *Conn, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("ws: panic in HandleClose",
				logger.Int64("conn_id", int64(conn.id)),
				logger.Any("panic", recovered))
		}
	}()
	s.handler.HandleClose(conn, err)
}

// isNormalClose reports whether err is a clean WebSocket shutdown rather than a
// transport failure.
func isNormalClose(err error) bool {
	var closeErr *gws.CloseError
	if !errors.As(err, &closeErr) {
		return false
	}
	switch closeErr.Code {
	case gws.CloseNormalClosure, gws.CloseGoingAway, gws.CloseNoStatusReceived:
		return true
	default:
		return false
	}
}

// startPing starts the heartbeat goroutine.
func (s *Server) startPing(conn *Conn) {
	// Set the Pong handler so that a received Pong resets the read timeout
	conn.ws.SetPongHandler(func(string) error {
		_ = conn.ws.SetReadDeadline(time.Now().Add(s.cfg.PingTimeout))
		return nil
	})

	go func() {
		// fillDefault guarantees a positive interval; guard anyway because
		// time.NewTicker panics on a non-positive duration.
		if s.cfg.PingInterval <= 0 {
			return
		}
		ticker := time.NewTicker(s.cfg.PingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-conn.done:
				return
			case <-ticker.C:
				if err := conn.WriteMessage(PingMessage, nil); err != nil {
					// A failed write leaves the connection unusable, but the read loop
					// may still be blocked on a client that keeps sending data (which
					// refreshes the read deadline). Close it so teardown runs.
					_ = conn.Close()
					return
				}
			}
		}
	}()
}

// --- Connection management ---

// GetConn returns the connection with the given ID.
// It only returns connections of this instance; connections of other cluster instances
// return false.
func (s *Server) GetConn(id ConnID) (*Conn, bool) {
	v, ok := s.conns.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*Conn), true
}

// Count returns the number of online connections.
func (s *Server) Count() int {
	count := 0
	s.conns.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// CloseConn closes the given connection.
func (s *Server) CloseConn(id ConnID) error {
	conn, ok := s.GetConn(id)
	if !ok {
		return errors.New("ws: connection not found")
	}
	return conn.Close()
}

// Room returns the room manager.
func (s *Server) Room() Room {
	return s.room
}

// Config returns the server configuration.
func (s *Server) Config() Config {
	return s.cfg
}

// NodeID returns the ID of the current node.
func (s *Server) NodeID() uint16 {
	return s.nodeID
}

// --- Broadcast ---

// To creates a broadcaster that sends messages to the given rooms.
//
// Standalone mode: iterate the local connections in the rooms.
// Cluster mode: broadcast the message to all instances over Redis Pub/Sub; every
// instance then dispatches it to its local connections in the target rooms.
//
// Usage:
//
//	srv.To("room1", "room2").PushText("hello")
//	srv.To("room1").Emit("event", data)
func (s *Server) To(rooms ...string) *Broadcaster {
	return &Broadcaster{server: s, targets: rooms}
}

// Broadcast broadcasts a text message to all connections.
// In cluster mode it is broadcast to all instances over Redis Pub/Sub.
func (s *Server) Broadcast(data []byte) {
	if s.cluster != nil {
		// Cluster mode: broadcast over Pub/Sub
		msg := clusterMessage{
			Type:        clusterMessageTypeBroadcast,
			MessageType: TextMessage,
			Data:        data,
		}
		_ = s.cluster.Publish(context.Background(), msg)
		return
	}

	// Standalone mode: iterate the local connections
	s.conns.Range(func(_, v any) bool {
		conn := v.(*Conn)
		if err := conn.WriteText(data); err != nil {
			// Evict the broken connection; otherwise it stays registered and every
			// later broadcast pays for another failed write.
			_ = conn.Close()
		}
		return true
	})
}

// BroadcastText broadcasts a string text message to all connections.
func (s *Server) BroadcastText(data string) {
	s.Broadcast([]byte(data))
}

// BroadcastJSON broadcasts a JSON message to all connections.
func (s *Server) BroadcastJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.Broadcast(data)
	return nil
}

// BroadcastEvent broadcasts an event to all connections.
func (s *Server) BroadcastEvent(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return s.BroadcastJSON(e)
}

// --- Shutdown ---

// Close shuts down the server, disconnecting all connections and releasing resources.
//
// Room cleanup is **limited to the connections of this instance**: the room keys of
// RedisRoom are shared by all instances, so clearing the whole batch of keys here would
// wipe the room memberships of the other nodes and make their later broadcasts fail
// silently (the objects are no longer in the room).
// Call RedisRoom.Clear explicitly when the rooms must be cleared as a whole (e.g. an
// operations reset).
func (s *Server) Close() error {
	// Refuse new connections first: a request that already upgraded must not be
	// able to register after the connection sweep below.
	s.closed.Store(true)

	// Stop listening on the cluster
	if s.cluster != nil {
		s.cluster.Stop()
	}

	// Remove this instance's connections from all rooms first (this must happen before
	// the Redis client is closed)
	s.conns.Range(func(_, v any) bool {
		s.room.Delete(v.(*Conn).ID())
		return true
	})

	// Close all connections concurrently: every Close sends a Close frame with its
	// own deadline, so closing them one by one could take up to one second per
	// unresponsive client.
	var wg sync.WaitGroup
	s.conns.Range(func(_, v any) bool {
		conn := v.(*Conn)
		wg.Add(1)
		go func(c *Conn) {
			defer wg.Done()
			_ = c.Close()
		}(conn)
		return true
	})
	wg.Wait()

	// Close the Redis client created by the Server
	if s.ownRedis && s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			return err
		}
	}

	return nil
}
