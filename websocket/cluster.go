package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
)

// 集群消息类型常量。
const (
	clusterChannelPattern = "%scluster"
	clusterMessageTypeBroadcast = "broadcast"
	clusterMessageTypeRoom     = "room"
)

// clusterMessage 集群内部广播消息。
// 通过 Redis Pub/Sub 在实例间传递，实现跨实例广播。
type clusterMessage struct {
	Type      string `json:"type"`       // broadcast（广播全部）或 room（定向房间）
	MessageType int   `json:"mt"`        // WebSocket 消息类型（TextMessage 等）
	Data      []byte `json:"data"`       // 消息内容
	Rooms     []string `json:"rooms,omitempty"` // 目标房间（仅 Type=room 时有效）
}

// ClusterHandler 集群广播处理器。
// 在 Redis 房间模式下，负责通过 Redis Pub/Sub 实现跨实例消息投递。
//
// 工作原理：
//  1. 本实例调用 To("room1").PushText("hello") 时
//  2. Broadcaster 先将消息通过 Redis PUBLISH 发送到集群频道
//  3. 所有实例（包括自身）的 ClusterHandler 收到消息后
//  4. 根据 type 字段分发到本实例的本地连接
type ClusterHandler struct {
	server   *Server
	nodeID   uint16
	pubsub   PubSub
	done     chan struct{}
	cancel   func() // 取消订阅
	wg       sync.WaitGroup
}

// PubSub 定义 Pub/Sub 所需的最小接口。
// 由 redisClusterBridge 实现，桥接 go-redis 的 PubSub。
type PubSub interface {
	// Publish 发布消息到指定频道。
	Publish(ctx context.Context, channel string, message []byte) error
	// Subscribe 订阅频道，返回消息通道，channel 在订阅就绪后关闭。
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// NewClusterHandler 创建集群处理器。
func NewClusterHandler(server *Server, nodeID uint16, pubsub PubSub) *ClusterHandler {
	return &ClusterHandler{
		server: server,
		nodeID: nodeID,
		pubsub: pubsub,
		done:   make(chan struct{}),
	}
}

// channel 返回集群频道名称。
func (h *ClusterHandler) channel() string {
	return fmt.Sprintf(clusterChannelPattern, h.server.cfg.RoomPrefix)
}

// Start 启动集群监听。
func (h *ClusterHandler) Start() error {
	ctx := context.Background()
	ch, cancel, err := h.pubsub.Subscribe(ctx, h.channel())
	if err != nil {
		return fmt.Errorf("websocket: failed to subscribe cluster channel: %w", err)
	}
	h.cancel = cancel

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-h.done:
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				h.handleMessage(data)
			}
		}
	}()

	return nil
}

// Stop 停止集群监听。
func (h *ClusterHandler) Stop() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

// Publish 广播消息到集群。
// 调用后所有实例（包括本实例）都会收到消息并分发给本地连接。
func (h *ClusterHandler) Publish(ctx context.Context, msg clusterMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return h.pubsub.Publish(ctx, h.channel(), data)
}

// handleMessage 处理收到的集群消息，分发给本实例的本地连接。
func (h *ClusterHandler) handleMessage(data []byte) {
	var msg clusterMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		if l := h.server.logger; l != nil {
			l.Warn("websocket: failed to unmarshal cluster message", logger.Err(err))
		}
		return
	}

	switch msg.Type {
	case clusterMessageTypeBroadcast:
		// 广播到本实例的所有连接
		h.server.conns.Range(func(_, v any) bool {
			conn := v.(*Conn)
			_ = conn.WriteMessage(msg.MessageType, msg.Data)
			return true
		})

	case clusterMessageTypeRoom:
		// 定向推送到本实例在目标房间内的连接
		fdSet := make(map[ConnID]struct{})
		for _, room := range msg.Rooms {
			for _, fd := range h.server.room.GetClients(room) {
				fdSet[fd] = struct{}{}
			}
		}
		for fd := range fdSet {
			if conn, ok := h.server.GetConn(fd); ok {
				_ = conn.WriteMessage(msg.MessageType, msg.Data)
			}
		}
	}
}
