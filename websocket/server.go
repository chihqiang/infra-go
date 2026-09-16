package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chihqiang/infra-go/logger"
	gws "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// Option is a server configuration option.
type Option func(*options)

// options holds the internal server options.
type options struct {
	logger       logger.ILogger
	redisClient  RedisClient
	pubsub       PubSub
	checkOrigin  func(r *http.Request) bool
	subprotocols []string
}

// WithLogger sets the logger; logger.GetGlobal() is used by default.
func WithLogger(l logger.ILogger) Option {
	return func(o *options) { o.logger = l }
}

// WithRedisClient sets the Redis client (used by Redis rooms).
// When it is not set and RoomType is "redis", one is created automatically from the
// Redis settings in Config.
func WithRedisClient(client RedisClient) Option {
	return func(o *options) { o.redisClient = client }
}

// WithCheckOrigin sets the Origin check function.
// All origins are allowed by default (suitable for development); production should
// configure a whitelist.
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return func(o *options) { o.checkOrigin = fn }
}

// WithSubprotocols sets the list of subprotocols to negotiate.
func WithSubprotocols(protocols ...string) Option {
	return func(o *options) { o.subprotocols = protocols }
}

// WithPubSub sets a custom PubSub implementation (used for cluster broadcast).
// In Redis room mode a go-redis based implementation is created automatically by default.
// Tests can inject a mock implementation through this option.
func WithPubSub(ps PubSub) Option {
	return func(o *options) { o.pubsub = ps }
}

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
//	h := websocket.NewEventHandler()
//	h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
//	    conn.Server().Broadcast([]byte("new message"))
//	})
//	srv := websocket.MustNew(websocket.Config{}, h)
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
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	if opt.checkOrigin != nil {
		upgrader.CheckOrigin = opt.checkOrigin
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
		s.logger.Error("websocket: upgrade failed", logger.Err(err))
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

	// Start the heartbeat
	s.startPing(conn)

	// Invoke OnOpen
	s.handler.HandleOpen(conn)

	// Read loop
	defer func() {
		s.handler.HandleClose(conn, nil)
		s.conns.Delete(id)
		s.room.Delete(id)
		conn.Close()
	}()

	for {
		// Set the read timeout (based on the heartbeat)
		_ = ws.SetReadDeadline(time.Now().Add(s.cfg.PingTimeout))

		messageType, data, err := ws.ReadMessage()
		if err != nil {
			// Check whether this is a normal close
			var ce *gws.CloseError
			if errors.As(err, &ce) {
				s.logger.Info("websocket: connection closed",
					logger.Int64("conn_id", int64(id)),
					logger.Int("code", ce.Code))
			}
			s.handler.HandleError(conn, err)
			return
		}

		s.handler.HandleMessage(conn, messageType, data)
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
		ticker := time.NewTicker(s.cfg.PingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-conn.done:
				return
			case <-ticker.C:
				if err := conn.WriteMessage(PingMessage, nil); err != nil {
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
		return errors.New("websocket: connection not found")
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
		_ = conn.WriteText(data)
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

	// Close all connections
	s.conns.Range(func(_, v any) bool {
		conn := v.(*Conn)
		conn.Close()
		return true
	})

	// Close the Redis client created by the Server
	if s.ownRedis && s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			return err
		}
	}

	return nil
}
