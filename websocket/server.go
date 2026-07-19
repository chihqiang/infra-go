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

// Option 服务器配置选项。
type Option func(*options)

// options 服务器内部选项。
type options struct {
	logger       logger.ILogger
	redisClient  RedisClient
	pubsub       PubSub
	checkOrigin  func(r *http.Request) bool
	subprotocols []string
}

// WithLogger 设置日志记录器，默认使用 logger.GetGlobal()。
func WithLogger(l logger.ILogger) Option {
	return func(o *options) { o.logger = l }
}

// WithRedisClient 设置 Redis 客户端（用于 Redis 房间）。
// 如果未设置，且 RoomType 为 "redis"，则根据 Config 中的 Redis 配置自动创建。
func WithRedisClient(client RedisClient) Option {
	return func(o *options) { o.redisClient = client }
}

// WithCheckOrigin 设置 Origin 检查函数。
// 默认允许所有来源（适合开发环境），生产环境应配置为白名单。
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return func(o *options) { o.checkOrigin = fn }
}

// WithSubprotocols 设置子协议协商列表。
func WithSubprotocols(protocols ...string) Option {
	return func(o *options) { o.subprotocols = protocols }
}

// WithPubSub 设置自定义 PubSub 实现（用于集群广播）。
// 默认在 Redis 房间模式下自动创建基于 go-redis 的 PubSub。
// 测试时可通过此选项注入 mock 实现。
func WithPubSub(ps PubSub) Option {
	return func(o *options) { o.pubsub = ps }
}

// Server WebSocket 服务器。
// 实现 http.Handler 接口，可直接用于 http.HandleFunc 或 http.Server。
//
// 核心职责：
//   - HTTP → WebSocket 升级
//   - 连接生命周期管理（创建、读取、关闭）
//   - 心跳检测（Ping/Pong）
//   - 房间管理
//   - 广播消息（单机 + 集群）
//
// 用法：
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
	counter atomic.Uint64 // 本地连接计数器（不含 NodeID 前缀）

	redisClient RedisClient // 由 Server 创建的 Redis 客户端（需在 Close 时关闭）
	ownRedis    bool        // 是否由 Server 创建的 Redis 客户端
	logger      logger.ILogger

	// 集群支持
	cluster *ClusterHandler // 集群处理器，非 nil 表示启用集群模式
	nodeID  uint16          // 节点 ID
}

// New 创建 WebSocket 服务器。
func New(cfg Config, handler Handler, opts ...Option) (*Server, error) {
	c := fillDefault(cfg)

	var opt options
	for _, o := range opts {
		o(&opt)
	}

	// 创建房间
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

	// 创建集群处理器
	// 当 RoomType 为 redis 时自动启用，或当通过 WithPubSub 传入自定义 PubSub 时启用
	var cluster *ClusterHandler
	if opt.pubsub != nil {
		cluster = NewClusterHandler(nil, c.NodeID, opt.pubsub)
	} else if c.RoomType == roomTypeRedis && redisClient != nil {
		cluster = NewClusterHandler(nil, c.NodeID, NewRedisPubSub(redisClient))
	}

	// 创建 Upgrader
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

	// 启动集群监听
	if cluster != nil {
		cluster.server = s
		if err := cluster.Start(); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// MustNew 创建 WebSocket 服务器，出错时 panic。
func MustNew(cfg Config, handler Handler, opts ...Option) *Server {
	s, err := New(cfg, handler, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// nextConnID 生成全局唯一的连接 ID。
// 集群模式下编码为 nodeID<<32 | localCounter，保证不同实例的 ID 不重叠。
func (s *Server) nextConnID() ConnID {
	local := s.counter.Add(1)
	return ConnID(s.nodeID)<<32 | ConnID(local)
}

// nodeIDFromConnID 从连接 ID 中提取节点 ID。
func nodeIDFromConnID(id ConnID) uint16 {
	return uint16(id >> 32)
}

// localIDFromConnID 从连接 ID 中提取本地计数器部分。
func localIDFromConnID(id ConnID) uint32 {
	return uint32(id & 0xFFFFFFFF)
}

// --- http.Handler 实现 ---

// ServeHTTP 处理 HTTP 请求，将连接升级为 WebSocket。
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

	// 设置消息大小限制
	if s.cfg.MaxMessageSize > 0 {
		ws.SetReadLimit(s.cfg.MaxMessageSize)
	}

	// 注册到连接表
	s.conns.Store(id, conn)

	// 启动心跳
	s.startPing(conn)

	// 调用 OnOpen
	s.handler.HandleOpen(conn)

	// 读取循环
	defer func() {
		s.handler.HandleClose(conn, nil)
		s.conns.Delete(id)
		s.room.Delete(id)
		conn.Close()
	}()

	for {
		// 设置读超时（基于心跳）
		_ = ws.SetReadDeadline(time.Now().Add(s.cfg.PingTimeout))

		messageType, data, err := ws.ReadMessage()
		if err != nil {
			// 判断是否为正常关闭
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

// startPing 启动心跳 goroutine。
func (s *Server) startPing(conn *Conn) {
	// 设置 Pong 处理器，收到 Pong 后重置读超时
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

// --- 连接管理 ---

// GetConn 根据连接 ID 获取连接。
// 仅返回本实例的连接，集群中其他实例的连接返回 false。
func (s *Server) GetConn(id ConnID) (*Conn, bool) {
	v, ok := s.conns.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*Conn), true
}

// Count 返回当前在线连接数。
func (s *Server) Count() int {
	count := 0
	s.conns.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// CloseConn 关闭指定连接。
func (s *Server) CloseConn(id ConnID) error {
	conn, ok := s.GetConn(id)
	if !ok {
		return errors.New("websocket: connection not found")
	}
	return conn.Close()
}

// Room 返回房间管理器。
func (s *Server) Room() Room {
	return s.room
}

// Config 返回服务器配置。
func (s *Server) Config() Config {
	return s.cfg
}

// NodeID 返回当前节点的 ID。
func (s *Server) NodeID() uint16 {
	return s.nodeID
}

// --- 广播 ---

// To 创建广播器，将消息发送到指定房间。
//
// 单机模式：直接遍历房间内的本实例连接。
// 集群模式：通过 Redis Pub/Sub 将消息广播到所有实例，
// 各实例收到后分发给本地在目标房间内的连接。
//
// 用法：
//
//	srv.To("room1", "room2").PushText("hello")
//	srv.To("room1").Emit("event", data)
func (s *Server) To(rooms ...string) *Broadcaster {
	return &Broadcaster{server: s, targets: rooms}
}

// Broadcast 广播文本消息到所有连接。
// 集群模式下会通过 Redis Pub/Sub 广播到所有实例。
func (s *Server) Broadcast(data []byte) {
	if s.cluster != nil {
		// 集群模式：通过 Pub/Sub 广播
		msg := clusterMessage{
			Type:        clusterMessageTypeBroadcast,
			MessageType: TextMessage,
			Data:        data,
		}
		_ = s.cluster.Publish(context.Background(), msg)
		return
	}

	// 单机模式：直接遍历本地连接
	s.conns.Range(func(_, v any) bool {
		conn := v.(*Conn)
		_ = conn.WriteText(data)
		return true
	})
}

// BroadcastText 广播字符串文本消息到所有连接。
func (s *Server) BroadcastText(data string) {
	s.Broadcast([]byte(data))
}

// BroadcastJSON 广播 JSON 消息到所有连接。
func (s *Server) BroadcastJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.Broadcast(data)
	return nil
}

// BroadcastEvent 广播事件到所有连接。
func (s *Server) BroadcastEvent(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return s.BroadcastJSON(e)
}

// --- 关闭 ---

// Close 关闭服务器，断开所有连接并清理资源。
func (s *Server) Close() error {
	// 停止集群监听
	if s.cluster != nil {
		s.cluster.Stop()
	}

	// 关闭所有连接
	s.conns.Range(func(_, v any) bool {
		conn := v.(*Conn)
		conn.Close()
		return true
	})

	// 清理房间
	s.room.Clear()

	// 关闭由 Server 创建的 Redis 客户端
	if s.ownRedis && s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			return err
		}
	}

	return nil
}
