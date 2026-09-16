package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
)

// Cluster message type constants.
const (
	clusterChannelPattern       = "%scluster"
	clusterMessageTypeBroadcast = "broadcast"
	clusterMessageTypeRoom      = "room"
)

// clusterMessage is an internal cluster broadcast message.
// It travels between instances over Redis Pub/Sub to implement cross-instance broadcast.
type clusterMessage struct {
	Type        string   `json:"type"`            // broadcast (all) or room (targeted)
	MessageType int      `json:"mt"`              // WebSocket message type (TextMessage, etc.)
	Data        []byte   `json:"data"`            // message payload
	Rooms       []string `json:"rooms,omitempty"` // target rooms (only used when Type=room)
}

// ClusterHandler handles cluster broadcasts.
// In Redis room mode it delivers messages across instances over Redis Pub/Sub.
//
// How it works:
//  1. When this instance calls To("room1").PushText("hello")
//  2. Broadcaster publishes the message to the cluster channel with Redis PUBLISH
//  3. The ClusterHandler of every instance (including this one) receives the message
//  4. It dispatches the message to the local connections based on the type field
type ClusterHandler struct {
	server *Server
	nodeID uint16
	pubsub PubSub
	done   chan struct{}
	cancel func() // cancels the subscription
	wg     sync.WaitGroup
}

// PubSub defines the minimal interface required by Pub/Sub.
// It is implemented by redisClusterBridge, which bridges the go-redis PubSub.
type PubSub interface {
	// Publish publishes a message to the given channel.
	Publish(ctx context.Context, channel string, message []byte) error
	// Subscribe subscribes to the channel and returns the message channel; channel is
	// closed once the subscription is released.
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// NewClusterHandler creates a cluster handler.
func NewClusterHandler(server *Server, nodeID uint16, pubsub PubSub) *ClusterHandler {
	return &ClusterHandler{
		server: server,
		nodeID: nodeID,
		pubsub: pubsub,
		done:   make(chan struct{}),
	}
}

// channel returns the cluster channel name.
func (h *ClusterHandler) channel() string {
	return fmt.Sprintf(clusterChannelPattern, h.server.cfg.RoomPrefix)
}

// Start starts listening on the cluster.
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

// Stop stops listening on the cluster.
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

// Publish broadcasts a message to the cluster.
// After it returns, every instance (including this one) receives the message and
// dispatches it to its local connections.
func (h *ClusterHandler) Publish(ctx context.Context, msg clusterMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return h.pubsub.Publish(ctx, h.channel(), data)
}

// handleMessage handles a received cluster message and dispatches it to the local
// connections of this instance.
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
		// Broadcast to all connections of this instance
		h.server.conns.Range(func(_, v any) bool {
			conn := v.(*Conn)
			_ = conn.WriteMessage(msg.MessageType, msg.Data)
			return true
		})

	case clusterMessageTypeRoom:
		// Deliver to this instance's connections in the target rooms
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
